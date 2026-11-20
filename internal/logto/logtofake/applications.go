package logtofake

import (
	"net/http"

	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

// Application is the subset of a Logto application the fake stores.
type Application struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// SeedApplication stores an application as if it already existed in Logto.
func (s *Server) SeedApplication(a Application) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a.ID == "" {
		a.ID = s.nextID("app")
	}
	s.applications = append(s.applications, a)
}

// ListApplications is Logto's GET /api/applications, paginated (filters not modelled yet).
func (s *Server) ListApplications(w http.ResponseWriter, r *http.Request, _ mgmt.ListApplicationsParams) {
	s.mu.Lock()
	out := make([]map[string]any, 0, len(s.applications))
	for _, a := range s.applications {
		out = append(out, map[string]any{
			"id": a.ID, "name": a.Name, "type": a.Type, "tenantId": "default",
			"oidcClientMetadata":   map[string]any{"redirectUris": []string{}, "postLogoutRedirectUris": []string{}},
			"customClientMetadata": map[string]any{},
		})
	}
	s.mu.Unlock()
	paginate(w, r, out)
}
