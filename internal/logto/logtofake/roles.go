package logtofake

import (
	"net/http"

	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

// ManagementRole is the built-in role that grants Management API access to a machine-to-machine application.
const ManagementRole = "Logto Management API access"

func (s *Server) initRoles() {
	if s.roles != nil {
		return
	}
	s.initResources()
	s.roles = newTable("role", "name")
	s.roleScopes = map[string][]string{}
	// Logto ships this role, holding the `all` scope of its Management API.
	s.roles.rows["role-mgmt"] = object{
		"tenantId": "default", "id": "role-mgmt", "name": ManagementRole, "description": "This default role grants access to the Logto management API.",
		"type": "MachineToMachine", "isDefault": false,
	}
	s.roleScopes["role-mgmt"] = []string{"management-api-all"}
}

// SeedRole stores a global role.
func (s *Server) SeedRole(name, typ string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initRoles()
	id := s.nextID("role")
	s.roles.rows[id] = object{"tenantId": "default", "id": id, "name": name, "description": name, "type": typ, "isDefault": false}
	return id
}

// Role is a global role and the scopes it holds ("<indicator> <scope>"), for test assertions.
type Role struct {
	ID, Name, Description, Type string
	IsDefault                   bool
	Scopes                      []string
}

// Roles returns the stored roles by name.
func (s *Server) Roles() map[string]Role {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initRoles()
	out := map[string]Role{}
	for id, o := range s.roles.rows {
		r := Role{ID: id, Name: str(o, "name"), Description: str(o, "description"), Type: str(o, "type")}
		r.IsDefault, _ = o["isDefault"].(bool)
		for _, sid := range s.roleScopes[id] {
			if sc, ok := s.scopeByID(sid); ok {
				r.Scopes = append(r.Scopes, str(s.resources[str(sc, "resourceId")], "indicator")+" "+str(sc, "name"))
			}
		}
		out[r.Name] = r
	}
	return out
}

func roleView(o object) object {
	v := copyObject(o)
	v["usersCount"], v["featuredUsers"], v["applicationsCount"], v["featuredApplications"] = 0, []any{}, 0, []any{}
	return v
}

// ListRoles is GET /api/roles, paginated.
func (s *Server) ListRoles(w http.ResponseWriter, r *http.Request, _ mgmt.ListRolesParams) {
	s.mu.Lock()
	s.initRoles()
	items := []object{}
	for _, o := range s.roles.list() {
		items = append(items, roleView(o))
	}
	s.mu.Unlock()
	paginate(w, r, items)
}

// CreateRole is POST /api/roles (answered 200, like Logto); a name in use answers 422, an unknown scope id 404.
func (s *Server) CreateRole(w http.ResponseWriter, r *http.Request) {
	body, ok := decode(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initRoles()
	scopes := stringList(body, "scopeIds")
	for _, sid := range scopes {
		if _, ok := s.scopeByID(sid); !ok {
			notFound(w)
			return
		}
	}
	typ := str(body, "type")
	if typ == "" {
		typ = "User"
	}
	isDefault, _ := body["isDefault"].(bool)
	o := object{"tenantId": "default", "name": str(body, "name"), "description": str(body, "description"), "type": typ, "isDefault": isDefault}
	if !s.roles.insert(s, w, o) {
		return
	}
	s.roleScopes[str(o, "id")] = scopes
	writeJSON(w, http.StatusOK, o)
}

// UpdateRole is PATCH /api/roles/{id}; the type is not in the body and cannot change.
func (s *Server) UpdateRole(w http.ResponseWriter, r *http.Request, id mgmt.RoleIdRoot) {
	body, ok := decode(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initRoles()
	if o, ok := s.roles.patch(w, id, body); ok {
		writeJSON(w, http.StatusOK, o)
	}
}

// DeleteRole is DELETE /api/roles/{id}; applications and users lose it.
func (s *Server) DeleteRole(w http.ResponseWriter, _ *http.Request, id mgmt.RoleIdRoot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initRoles()
	if !s.roles.remove(w, id) {
		return
	}
	delete(s.roleScopes, id)
	for app, list := range s.appRoles {
		s.appRoles[app] = without(list, id)
	}
	for user, list := range s.userRoles {
		s.userRoles[user] = without(list, id)
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListRoleScopes is GET /api/roles/{id}/scopes: each scope carries its resource.
func (s *Server) ListRoleScopes(w http.ResponseWriter, r *http.Request, id mgmt.RoleIdRoot, _ mgmt.ListRoleScopesParams) {
	s.mu.Lock()
	s.initRoles()
	_, ok := s.roles.rows[id]
	items := s.roleScopeViews(id)
	s.mu.Unlock()
	if !ok {
		notFound(w)
		return
	}
	paginate(w, r, items)
}

func (s *Server) roleScopeViews(id string) []object {
	items := []object{}
	for _, sid := range s.roleScopes[id] {
		if sc, ok := s.scopeByID(sid); ok {
			v := copyObject(sc)
			v["resource"] = copyObject(s.resources[str(sc, "resourceId")])
			items = append(items, v)
		}
	}
	return items
}

// CreateRoleScope is POST /api/roles/{id}/scopes: the scopes are added (a scope the role holds answers 422, like
// Logto), and the role's scopes are answered with 201, without their resource.
func (s *Server) CreateRoleScope(w http.ResponseWriter, r *http.Request, id mgmt.RoleIdRoot) {
	body, ok := decode(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initRoles()
	if _, ok := s.roles.get(w, id); !ok {
		return
	}
	add := stringList(body, "scopeIds")
	for _, sid := range add {
		if _, ok := s.scopeByID(sid); !ok {
			notFound(w)
			return
		}
		if contains(s.roleScopes[id], sid) {
			conflict(w, "role.scope_exists")
			return
		}
	}
	s.roleScopes[id] = append(s.roleScopes[id], add...)
	out := []object{}
	for _, sid := range s.roleScopes[id] {
		sc, _ := s.scopeByID(sid)
		out = append(out, sc)
	}
	writeJSON(w, http.StatusCreated, out)
}

// DeleteRoleScope is DELETE /api/roles/{id}/scopes/{scopeId}.
func (s *Server) DeleteRoleScope(w http.ResponseWriter, _ *http.Request, id mgmt.RoleIdRoot, scopeID mgmt.ScopeId) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initRoles()
	if _, ok := s.roles.get(w, id); !ok {
		return
	}
	if !contains(s.roleScopes[id], scopeID) {
		notFound(w)
		return
	}
	s.roleScopes[id] = without(s.roleScopes[id], scopeID)
	w.WriteHeader(http.StatusNoContent)
}

// dropRoleScopes removes deleted resource scopes from every role, as Logto's foreign keys do.
func (s *Server) dropRoleScopes(ids ...string) {
	s.initRoles()
	for role, list := range s.roleScopes {
		for _, id := range ids {
			list = without(list, id)
		}
		s.roleScopes[role] = list
	}
}
