package logtofake

import (
	"net/http"

	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

func (s *Server) initOrganizations() {
	if s.orgScopes != nil {
		return
	}
	s.orgScopes = map[string]object{}
	s.orgRoles = map[string]object{}
	s.orgRoleScopes = map[string][]string{}
	s.orgRoleResourceScopes = map[string][]string{}
}

// OrganizationRole is an organization role and the names of the scopes it holds, for test assertions.
type OrganizationRole struct {
	Name, Description string
	Scopes            []string // organization scope names
	ResourceScopes    []string // resource scope names
}

// OrganizationRoles returns the stored roles by name.
func (s *Server) OrganizationRoles() map[string]OrganizationRole {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initOrganizations()
	out := map[string]OrganizationRole{}
	for id, o := range s.orgRoles {
		role := OrganizationRole{Name: str(o, "name"), Description: str(o, "description")}
		for _, sid := range s.orgRoleScopes[id] {
			role.Scopes = append(role.Scopes, str(s.orgScopes[sid], "name"))
		}
		for _, sid := range s.orgRoleResourceScopes[id] {
			if sc, ok := s.scopeByID(sid); ok {
				role.ResourceScopes = append(role.ResourceScopes, str(sc, "name"))
			}
		}
		out[role.Name] = role
	}
	return out
}

// OrganizationScopeNames returns the stored organization scope names.
func (s *Server) OrganizationScopeNames() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initOrganizations()
	var out []string
	for _, o := range sorted(s.orgScopes) {
		out = append(out, str(o, "name"))
	}
	return out
}

// ListOrganizationScopes is GET /api/organization-scopes.
func (s *Server) ListOrganizationScopes(w http.ResponseWriter, r *http.Request, _ mgmt.ListOrganizationScopesParams) {
	s.mu.Lock()
	s.initOrganizations()
	items := sorted(s.orgScopes)
	s.mu.Unlock()
	paginate(w, r, items)
}

// CreateOrganizationScope is POST /api/organization-scopes; a name in use answers 422.
func (s *Server) CreateOrganizationScope(w http.ResponseWriter, r *http.Request) {
	body, ok := decode(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initOrganizations()
	for _, o := range s.orgScopes {
		if str(o, "name") == str(body, "name") {
			conflict(w, "entity.unique_integrity_violation")
			return
		}
	}
	id := s.nextID("os")
	o := object{"tenantId": "default", "id": id, "name": str(body, "name"), "description": body["description"]}
	s.orgScopes[id] = o
	writeJSON(w, http.StatusCreated, o)
}

// UpdateOrganizationScope is PATCH /api/organization-scopes/{id}.
func (s *Server) UpdateOrganizationScope(w http.ResponseWriter, r *http.Request, id mgmt.OrganizationScopeIdRoot) {
	body, ok := decode(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initOrganizations()
	o, ok := s.orgScopes[id]
	if !ok {
		notFound(w)
		return
	}
	merge(o, body)
	writeJSON(w, http.StatusOK, o)
}

// DeleteOrganizationScope is DELETE /api/organization-scopes/{id}; roles lose it.
func (s *Server) DeleteOrganizationScope(w http.ResponseWriter, _ *http.Request, id mgmt.OrganizationScopeIdRoot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initOrganizations()
	if _, ok := s.orgScopes[id]; !ok {
		notFound(w)
		return
	}
	delete(s.orgScopes, id)
	for rid, list := range s.orgRoleScopes {
		s.orgRoleScopes[rid] = without(list, id)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) roleView(id string) object {
	o := copyObject(s.orgRoles[id])
	scopes := []object{}
	for _, sid := range s.orgRoleScopes[id] {
		scopes = append(scopes, object{"id": sid, "name": str(s.orgScopes[sid], "name")})
	}
	resourceScopes := []object{}
	for _, sid := range s.orgRoleResourceScopes[id] {
		if sc, ok := s.scopeByID(sid); ok {
			res := s.resources[str(sc, "resourceId")]
			resourceScopes = append(resourceScopes, object{"id": sid, "name": str(sc, "name"), "resource": object{"id": str(res, "id"), "name": str(res, "name")}})
		}
	}
	o["scopes"], o["resourceScopes"] = scopes, resourceScopes
	return o
}

// ListOrganizationRoles is GET /api/organization-roles; each role carries its scopes and resource scopes.
func (s *Server) ListOrganizationRoles(w http.ResponseWriter, r *http.Request, _ mgmt.ListOrganizationRolesParams) {
	s.mu.Lock()
	s.initOrganizations()
	var items []object
	for _, o := range sorted(s.orgRoles) {
		items = append(items, s.roleView(str(o, "id")))
	}
	s.mu.Unlock()
	if items == nil {
		items = []object{}
	}
	paginate(w, r, items)
}

// CreateOrganizationRole is POST /api/organization-roles; a name in use answers 422.
func (s *Server) CreateOrganizationRole(w http.ResponseWriter, r *http.Request) {
	body, ok := decode(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initOrganizations()
	for _, o := range s.orgRoles {
		if str(o, "name") == str(body, "name") {
			conflict(w, "entity.unique_integrity_violation")
			return
		}
	}
	typ := str(body, "type")
	if typ == "" {
		typ = "User"
	}
	id := s.nextID("or")
	s.orgRoles[id] = object{"tenantId": "default", "id": id, "name": str(body, "name"), "description": body["description"], "type": typ}
	s.orgRoleScopes[id] = stringList(body, "organizationScopeIds")
	s.orgRoleResourceScopes[id] = stringList(body, "resourceScopeIds")
	writeJSON(w, http.StatusCreated, s.orgRoles[id])
}

// UpdateOrganizationRole is PATCH /api/organization-roles/{id}.
func (s *Server) UpdateOrganizationRole(w http.ResponseWriter, r *http.Request, id mgmt.OrganizationRoleIdRoot) {
	body, ok := decode(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initOrganizations()
	o, ok := s.orgRoles[id]
	if !ok {
		notFound(w)
		return
	}
	merge(o, body)
	writeJSON(w, http.StatusOK, o)
}

// DeleteOrganizationRole is DELETE /api/organization-roles/{id}.
func (s *Server) DeleteOrganizationRole(w http.ResponseWriter, _ *http.Request, id mgmt.OrganizationRoleIdRoot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initOrganizations()
	if _, ok := s.orgRoles[id]; !ok {
		notFound(w)
		return
	}
	delete(s.orgRoles, id)
	delete(s.orgRoleScopes, id)
	delete(s.orgRoleResourceScopes, id)
	w.WriteHeader(http.StatusNoContent)
}

// ReplaceOrganizationRoleScopes is PUT /api/organization-roles/{id}/scopes: the role holds exactly these.
func (s *Server) ReplaceOrganizationRoleScopes(w http.ResponseWriter, r *http.Request, id mgmt.OrganizationRoleIdRoot) {
	body, ok := decode(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initOrganizations()
	if _, ok := s.orgRoles[id]; !ok {
		notFound(w)
		return
	}
	s.orgRoleScopes[id] = stringList(body, "organizationScopeIds")
	w.WriteHeader(http.StatusNoContent)
}

// ReplaceOrganizationRoleResourceScopes is PUT /api/organization-roles/{id}/resource-scopes.
func (s *Server) ReplaceOrganizationRoleResourceScopes(w http.ResponseWriter, r *http.Request, id mgmt.OrganizationRoleIdRoot) {
	body, ok := decode(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initOrganizations()
	if _, ok := s.orgRoles[id]; !ok {
		notFound(w)
		return
	}
	s.orgRoleResourceScopes[id] = stringList(body, "scopeIds")
	w.WriteHeader(http.StatusNoContent)
}

func without(list []string, id string) []string {
	var out []string
	for _, v := range list {
		if v != id {
			out = append(out, v)
		}
	}
	return out
}
