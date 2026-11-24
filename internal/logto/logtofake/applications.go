package logtofake

import (
	"net/http"

	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

// Application is the subset of a Logto application a test seeds.
type Application struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	ThirdParty bool   `json:"isThirdParty"`
}

func (s *Server) initApplications() {
	if s.apps != nil {
		return
	}
	s.apps = map[string]object{}
	s.appSecrets = map[string][]object{}
	s.appRoles = map[string][]string{}
	s.appConsent = map[string]*consent{}
}

func newApplication(id, name, typ string) object {
	return object{
		"tenantId": "default", "id": id, "name": name, "description": nil, "type": typ,
		"oidcClientMetadata":   object{"redirectUris": []any{}, "postLogoutRedirectUris": []any{}},
		"customClientMetadata": object{}, "protectedAppMetadata": nil, "customData": object{},
		"isThirdParty": false, "appLevelAccessControlEnabled": false, "createdAt": now(),
	}
}

// SeedApplication stores an application as if it already existed in Logto.
func (s *Server) SeedApplication(a Application) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initApplications()
	if a.ID == "" {
		a.ID = s.nextID("app")
	}
	s.apps[a.ID] = newApplication(a.ID, a.Name, a.Type)
	s.apps[a.ID]["isThirdParty"] = a.ThirdParty
	if a.Type == "MachineToMachine" {
		s.appSecrets[a.ID] = []object{s.newSecret(a.ID, "Default secret")}
	}
}

// Applications returns the stored applications (copies).
func (s *Server) Applications() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initApplications()
	var out []map[string]any
	for _, o := range sorted(s.apps) {
		out = append(out, copyObject(o))
	}
	return out
}

// ApplicationSecrets returns the secret values of an application.
func (s *Server) ApplicationSecrets(id string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initApplications()
	var out []string
	for _, sec := range s.appSecrets[id] {
		out = append(out, str(sec, "value"))
	}
	return out
}

// ApplicationRoleNames returns the names of the roles granted to an application.
func (s *Server) ApplicationRoleNames(id string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initApplications()
	s.initRoles()
	var out []string
	for _, rid := range s.appRoles[id] {
		out = append(out, str(s.roles.rows[rid], "name"))
	}
	return out
}

func (s *Server) newSecret(appID, name string) object {
	return object{"tenantId": "default", "applicationId": appID, "name": name, "value": s.nextID("secret-value-"), "createdAt": now(), "expiresAt": nil}
}

// ListApplications is GET /api/applications, paginated.
func (s *Server) ListApplications(w http.ResponseWriter, r *http.Request, _ mgmt.ListApplicationsParams) {
	s.mu.Lock()
	s.initApplications()
	items := sorted(s.apps)
	s.mu.Unlock()
	if items == nil {
		items = []object{}
	}
	paginate(w, r, items)
}

// CreateApplication is POST /api/applications: Logto generates the id; a machine-to-machine application gets a
// "Default secret".
func (s *Server) CreateApplication(w http.ResponseWriter, r *http.Request) {
	body, ok := decode(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initApplications()
	id := s.nextID("app")
	o := newApplication(id, str(body, "name"), str(body, "type"))
	merge(o, body)
	s.apps[id] = o
	if str(o, "type") == "MachineToMachine" {
		s.appSecrets[id] = []object{s.newSecret(id, "Default secret")}
	}
	writeJSON(w, http.StatusOK, o)
}

// UpdateApplication is PATCH /api/applications/{id}: top-level keys replace the stored ones; type cannot change.
func (s *Server) UpdateApplication(w http.ResponseWriter, r *http.Request, id mgmt.ApplicationIdRoot) {
	body, ok := decode(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initApplications()
	o, ok := s.apps[id]
	if !ok {
		notFound(w)
		return
	}
	delete(body, "type")
	merge(o, body)
	writeJSON(w, http.StatusOK, o)
}

// ListApplicationSecrets is GET /api/applications/{id}/secrets.
func (s *Server) ListApplicationSecrets(w http.ResponseWriter, _ *http.Request, id mgmt.ApplicationIdRoot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initApplications()
	if _, ok := s.apps[id]; !ok {
		notFound(w)
		return
	}
	out := s.appSecrets[id]
	if out == nil {
		out = []object{}
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateApplicationSecret is POST /api/applications/{id}/secrets: Logto generates the value.
func (s *Server) CreateApplicationSecret(w http.ResponseWriter, r *http.Request, id mgmt.ApplicationIdRoot) {
	body, ok := decode(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initApplications()
	if _, ok := s.apps[id]; !ok {
		notFound(w)
		return
	}
	sec := s.newSecret(id, str(body, "name"))
	s.appSecrets[id] = append(s.appSecrets[id], sec)
	writeJSON(w, http.StatusCreated, sec)
}

// ListApplicationRoles is GET /api/applications/{applicationId}/roles.
func (s *Server) ListApplicationRoles(w http.ResponseWriter, r *http.Request, appID mgmt.ApplicationId, _ mgmt.ListApplicationRolesParams) {
	s.mu.Lock()
	s.initApplications()
	s.initRoles()
	items := []object{}
	for _, rid := range s.appRoles[appID] {
		items = append(items, s.roles.rows[rid])
	}
	s.mu.Unlock()
	paginate(w, r, items)
}

// AssignApplicationRoles is POST /api/applications/{applicationId}/roles: the roles are added.
func (s *Server) AssignApplicationRoles(w http.ResponseWriter, r *http.Request, appID mgmt.ApplicationId) {
	body, ok := decode(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initApplications()
	s.initRoles()
	if _, ok := s.apps[appID]; !ok {
		notFound(w)
		return
	}
	for _, rid := range stringList(body, "roleIds") {
		if _, ok := s.roles.rows[rid]; !ok {
			notFound(w)
			return
		}
		if !contains(s.appRoles[appID], rid) {
			s.appRoles[appID] = append(s.appRoles[appID], rid)
		}
	}
	w.WriteHeader(http.StatusCreated)
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
