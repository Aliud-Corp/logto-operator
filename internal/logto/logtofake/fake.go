// Package logtofake is an in-memory fake of Logto's Management API for the engine's unit tests.
//
// It is built ON the generated server (mgmt.ServerInterface): routing and parameter parsing come from the vendored
// OpenAPI document, and every operation the fake does not override answers 501 through mgmt.Unimplemented. A test can
// therefore never pass against a route that nobody modelled, and a Logto upgrade that renames or removes a route stops
// compiling the fake instead of silently diverging from it.
//
// Behaviour the fake models is Logto's as observed (see docs/plan.md); each override says which behaviour it
// reproduces, and a real-Logto integration test confirms it.
package logtofake

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
	"github.com/Aliud-Corp/logto-operator/internal/logto/openapi"
)

// Server is the fake. Its zero value is not usable: call New.
type Server struct {
	mgmt.Unimplemented

	clientID, clientSecret string

	mu       sync.Mutex
	tokenTTL time.Duration
	tokens   map[string]time.Time
	issued   int
	writes   int
	failures map[string][]int // path -> statuses to answer next, in order
	spec     []byte
	seq      int
	docs     map[string]*openapi.Doc // ValidateAgainst
	problems []string

	apps       map[string]object
	appSecrets map[string][]object // app id -> secrets
	appRoles   map[string][]string // app id -> role ids
	appConsent map[string]*consent // app id -> user consent scopes
	roles      *table
	roleScopes map[string][]string // role id -> resource scope ids

	resources             map[string]object
	resourceScopes        map[string]map[string]object // resource id -> scope id -> scope
	orgScopes             *table
	orgRoles              *table
	orgRoleScopes         map[string][]string // role id -> organization scope ids
	orgRoleResourceScopes map[string][]string // role id -> resource scope ids
	profileFields         map[string]object   // by name
	connectors            map[string]object   // by id
	signInExp             object
}

// New returns a fake that issues Management API tokens to the given client credentials only.
func New(clientID, clientSecret string) *Server {
	return &Server{
		clientID:     clientID,
		clientSecret: clientSecret,
		tokenTTL:     time.Hour,
		tokens:       map[string]time.Time{},
		failures:     map[string][]int{},
	}
}

// SetTokenTTL sets the lifetime of the tokens issued from now on.
func (s *Server) SetTokenTTL(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokenTTL = d
}

// TokensIssued is the number of tokens issued so far.
func (s *Server) TokensIssued() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.issued
}

// Writes is the number of successful non-GET Management API calls so far: a replay of an unchanged document must
// not move it.
func (s *Server) Writes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writes
}

// FailNext makes the next calls to path answer the given statuses, in order, before serving normally again. A path
// prefixed with a method ("POST /api/applications") fails only calls of that method.
func (s *Server) FailNext(path string, statuses ...int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures[path] = append(s.failures[path], statuses...)
}

// ServeSpec makes the fake serve a Logto OpenAPI document (a vendored one) as its own spec, like an instance does at
// /api/swagger.json and /api/.well-known/management.openapi.json, without authentication.
func (s *Server) ServeSpec(raw []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spec = raw
}

// Handler serves the OIDC token endpoint and the Management API under /api, bearer-protected like Logto's.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(s.injectFailures, s.validate, s.countWrites)
	r.Post("/oidc/token", s.issueToken)
	return mgmt.HandlerWithOptions(s, mgmt.ChiServerOptions{
		BaseRouter:  r,
		Middlewares: []mgmt.MiddlewareFunc{s.requireBearer},
	})
}

// issueToken is the client_credentials grant, with HTTP Basic client authentication (as Logto accepts it).
func (s *Server) issueToken(w http.ResponseWriter, r *http.Request) {
	id, secret, ok := r.BasicAuth()
	if err := r.ParseForm(); err != nil || !ok || id != s.clientID || secret != s.clientSecret ||
		r.PostForm.Get("grant_type") != "client_credentials" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_client"})
		return
	}
	s.mu.Lock()
	s.issued++
	token := fmt.Sprintf("fake-token-%d", s.issued)
	ttl := s.tokenTTL
	s.tokens[token] = time.Now().Add(ttl)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"access_token": token, "token_type": "Bearer", "expires_in": int(ttl.Seconds())})
}

var public = map[string]bool{"/api/swagger.json": true, "/api/.well-known/management.openapi.json": true, "/api/status": true}

func (s *Server) requireBearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if public[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		s.mu.Lock()
		expiry, ok := s.tokens[token]
		s.mu.Unlock()
		if !ok || time.Now().After(expiry) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "auth.unauthorized", "message": "invalid or expired token"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) injectFailures(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		status := 0
		for _, key := range []string{r.Method + " " + r.URL.Path, r.URL.Path} {
			if queue := s.failures[key]; len(queue) > 0 {
				status, s.failures[key] = queue[0], queue[1:]
				break
			}
		}
		s.mu.Unlock()
		if status != 0 {
			writeJSON(w, status, map[string]string{"code": "fake.injected", "message": http.StatusText(status)})
			return
		}
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (s *Server) countWrites(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if r.Method != http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/") && rec.status < 300 {
			s.mu.Lock()
			s.writes++
			s.mu.Unlock()
		}
	})
}

// GetSwaggerJson serves the configured spec (ServeSpec).
func (s *Server) GetSwaggerJson(w http.ResponseWriter, _ *http.Request) { //nolint:revive // generated name
	s.serveSpec(w)
}

// GetWellKnownManagementOpenapiJson serves the configured spec (ServeSpec).
func (s *Server) GetWellKnownManagementOpenapiJson(w http.ResponseWriter, _ *http.Request) { //nolint:revive // generated name
	s.serveSpec(w)
}

func (s *Server) serveSpec(w http.ResponseWriter) {
	s.mu.Lock()
	spec := s.spec
	s.mu.Unlock()
	if spec == nil {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"code": "fake.no_spec"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(spec)
}

// nextID returns a Logto-looking generated id.
func (s *Server) nextID(prefix string) string {
	s.seq++
	return fmt.Sprintf("%s%017d", prefix, s.seq)
}

// paginate answers a Logto list: page (from 1) and page_size (default 20) from the query, page_size above 100
// refused like Logto does (guard.invalid_pagination), the total in the Total-Number header.
func paginate[T any](w http.ResponseWriter, r *http.Request, items []T) {
	q := r.URL.Query()
	page, size := 1, 20
	if v := q.Get("page"); v != "" {
		page, _ = strconv.Atoi(v)
	}
	if v := q.Get("page_size"); v != "" {
		size, _ = strconv.Atoi(v)
	}
	if page < 1 || size < 1 || size > 100 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "guard.invalid_pagination", "message": "Invalid pagination value."})
		return
	}
	start := min((page-1)*size, len(items))
	end := min(start+size, len(items))
	w.Header().Set("Total-Number", strconv.Itoa(len(items)))
	writeJSON(w, http.StatusOK, items[start:end])
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
