// Package logtofake is an in-memory fake of Logto's Management API for the engine's unit tests.
//
// It is built ON the generated server (mgmt.ServerInterface): routing and parameter parsing come from the vendored
// OpenAPI document, and every operation the fake does not override answers 501 through mgmt.Unimplemented. A test can
// therefore never pass against a route that nobody modelled, and a Logto upgrade that renames or removes a route stops
// compiling the fake instead of silently diverging from it.
//
// Behaviour the fake models is Logto 1.44.0's as observed (see docs/plan.md); each override says which behaviour it
// reproduces, and a real-Logto integration test confirms it.
package logtofake

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"

	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

// Application is the subset of a Logto application the fake stores.
type Application struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// Server is the fake. Its zero value is not usable: call New.
type Server struct {
	mgmt.Unimplemented

	clientID, clientSecret, token string

	mu           sync.Mutex
	applications []Application
}

// New returns a fake that issues a Management API token to the given client credentials only.
func New(clientID, clientSecret string) *Server {
	return &Server{clientID: clientID, clientSecret: clientSecret, token: "fake-token-" + clientID}
}

// SeedApplication stores an application as if it already existed in Logto.
func (s *Server) SeedApplication(a Application) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applications = append(s.applications, a)
}

// Handler serves the OIDC token endpoint and the Management API under /api, bearer-protected like Logto's.
func (s *Server) Handler() http.Handler {
	r := chi.NewRouter()
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
	writeJSON(w, http.StatusOK, map[string]any{"access_token": s.token, "token_type": "Bearer", "expires_in": 3600})
}

func (s *Server) requireBearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") != s.token {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"code": "auth.authorization_header_missing"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ListApplications returns every stored application, like Logto's GET /api/applications (filters not modelled yet).
func (s *Server) ListApplications(w http.ResponseWriter, _ *http.Request, _ mgmt.ListApplicationsParams) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]map[string]any, 0, len(s.applications))
	for _, a := range s.applications {
		out = append(out, map[string]any{
			"id": a.ID, "name": a.Name, "type": a.Type, "tenantId": "default",
			"oidcClientMetadata":   map[string]any{"redirectUris": []string{}, "postLogoutRedirectUris": []string{}},
			"customClientMetadata": map[string]any{},
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
