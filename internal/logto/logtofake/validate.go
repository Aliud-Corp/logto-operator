package logtofake

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/Aliud-Corp/logto-operator/internal/logto/openapi"
)

// ValidateAgainst makes the fake check every request body against each given spec (a version of the support window),
// refusing an invalid one with 400 as Logto would, and check the fields of every answer it gives (types, declared
// keys). Every problem is kept for Problems: a kind test fails on one, so the bodies a kind really sends are valid
// for every supported Logto, and the fake answers what Logto's spec says.
func (s *Server) ValidateAgainst(docs map[string]*openapi.Doc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.docs = docs
}

// Problems are the requests and answers that did not fit a spec given to ValidateAgainst.
func (s *Server) Problems() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.problems...)
}

func (s *Server) problem(format string, args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.problems = append(s.problems, fmt.Sprintf(format, args...))
}

type bodyRecorder struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (r *bodyRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *bodyRecorder) Write(b []byte) (int, error) {
	r.body.Write(b)
	return r.ResponseWriter.Write(b)
}

func (s *Server) validate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		docs := s.docs
		s.mu.Unlock()
		// The spec routes serve a vendored document as it is: validating it against itself only costs time.
		if len(docs) == 0 || !strings.HasPrefix(r.URL.Path, "/api/") || public[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		versions := make([]string, 0, len(docs))
		for v := range docs {
			versions = append(versions, v)
		}
		sort.Strings(versions)
		call := r.Method + " " + r.URL.Path
		var body []byte
		if r.Body != nil {
			body, _ = io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		var refused []string
		ops := map[string]string{}
		for _, v := range versions {
			op, ok := docs[v].Find(r.Method, r.URL.Path)
			if !ok {
				refused = append(refused, v+": no such operation")
				continue
			}
			ops[v] = op
			if len(bytes.TrimSpace(body)) == 0 {
				continue
			}
			schema, err := docs[v].RequestSchema(op)
			if err != nil || schema == nil {
				continue
			}
			var value any
			if err := json.Unmarshal(body, &value); err != nil {
				refused = append(refused, v+": body is not JSON")
				continue
			}
			for _, p := range openapi.Validate(schema, value) {
				refused = append(refused, v+": "+p)
			}
		}
		if len(refused) > 0 {
			s.problem("request %s: %s", call, strings.Join(refused, "; "))
			writeJSON(w, http.StatusBadRequest, map[string]any{"code": "guard.invalid_input", "message": strings.Join(refused, "; ")})
			return
		}
		rec := &bodyRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if rec.status >= 300 || rec.status == http.StatusNoContent || rec.body.Len() == 0 {
			return
		}
		var answer any
		if err := json.Unmarshal(rec.body.Bytes(), &answer); err != nil {
			s.problem("answer %s: not JSON", call)
			return
		}
		var wrong []string
		for _, v := range versions {
			schema, err := docs[v].ResponseSchema(ops[v], rec.status)
			if err != nil {
				wrong = append(wrong, v+": "+err.Error())
				continue
			}
			if schema == nil {
				continue
			}
			for _, p := range openapi.Fields(schema, answer) {
				wrong = append(wrong, v+": "+p)
			}
		}
		if len(wrong) > 0 {
			s.problem("answer %s %d: %s", call, rec.status, strings.Join(wrong, "; "))
		}
	})
}
