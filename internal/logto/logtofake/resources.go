package logtofake

import (
	"net/http"

	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

// ManagementIndicator is the Management API resource Logto creates itself.
const ManagementIndicator = "https://default.logto.app/api"

func (s *Server) initResources() {
	if s.resources != nil {
		return
	}
	s.resources = map[string]object{}
	s.resourceScopes = map[string]map[string]object{}
	// Logto ships the Management API resource and its `all` scope.
	id := "management-api"
	s.resources[id] = object{"tenantId": "default", "id": id, "name": "Logto Management API", "indicator": ManagementIndicator, "isDefault": false, "accessTokenTtl": 3600}
	s.resourceScopes[id] = map[string]object{"management-api-all": {
		"tenantId": "default", "id": "management-api-all", "resourceId": id, "name": "all",
		"description": "Default scope for Management API, allows all permissions.", "createdAt": now(),
	}}
}

// Resources returns the stored resources (copies), for test assertions.
func (s *Server) Resources() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initResources()
	var out []map[string]any
	for _, o := range sorted(s.resources) {
		out = append(out, copyObject(o))
	}
	return out
}

// ResourceScopes returns the scope names of the resource with that indicator.
func (s *Server) ResourceScopes(indicator string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initResources()
	for id, r := range s.resources {
		if str(r, "indicator") == indicator {
			var names []string
			for _, sc := range sorted(s.resourceScopes[id]) {
				names = append(names, str(sc, "name"))
			}
			return names
		}
	}
	return nil
}

// ListResources is GET /api/resources.
func (s *Server) ListResources(w http.ResponseWriter, r *http.Request, _ mgmt.ListResourcesParams) {
	s.mu.Lock()
	s.initResources()
	items := sorted(s.resources)
	s.mu.Unlock()
	paginate(w, r, items)
}

// CreateResource is POST /api/resources; an indicator in use answers 422.
func (s *Server) CreateResource(w http.ResponseWriter, r *http.Request) {
	body, ok := decode(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initResources()
	for _, o := range s.resources {
		if str(o, "indicator") == str(body, "indicator") {
			conflict(w, "resource.resource_indicator_already_exists")
			return
		}
	}
	ttl := body["accessTokenTtl"]
	if ttl == nil {
		ttl = 3600
	}
	id := s.nextID("res")
	o := object{"tenantId": "default", "id": id, "name": str(body, "name"), "indicator": str(body, "indicator"), "isDefault": false, "accessTokenTtl": ttl}
	s.resources[id] = o
	s.resourceScopes[id] = map[string]object{}
	writeJSON(w, http.StatusCreated, o)
}

// UpdateResource is PATCH /api/resources/{id}.
func (s *Server) UpdateResource(w http.ResponseWriter, r *http.Request, id mgmt.ResourceIdRoot) {
	body, ok := decode(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initResources()
	o, ok := s.resources[id]
	if !ok {
		notFound(w)
		return
	}
	merge(o, body)
	writeJSON(w, http.StatusOK, o)
}

// DeleteResource is DELETE /api/resources/{id}.
func (s *Server) DeleteResource(w http.ResponseWriter, _ *http.Request, id mgmt.ResourceIdRoot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initResources()
	if _, ok := s.resources[id]; !ok {
		notFound(w)
		return
	}
	for sid := range s.resourceScopes[id] {
		s.dropRoleScopes(sid)
	}
	delete(s.resources, id)
	delete(s.resourceScopes, id)
	w.WriteHeader(http.StatusNoContent)
}

// ListResourceScopes is GET /api/resources/{resourceId}/scopes.
func (s *Server) ListResourceScopes(w http.ResponseWriter, r *http.Request, resourceID mgmt.ResourceId, _ mgmt.ListResourceScopesParams) {
	s.mu.Lock()
	s.initResources()
	scopes, ok := s.resourceScopes[resourceID]
	items := sorted(scopes)
	s.mu.Unlock()
	if !ok {
		notFound(w)
		return
	}
	paginate(w, r, items)
}

// CreateResourceScope is POST /api/resources/{resourceId}/scopes; a name in use answers 422.
func (s *Server) CreateResourceScope(w http.ResponseWriter, r *http.Request, resourceID mgmt.ResourceId) {
	body, ok := decode(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initResources()
	scopes, ok := s.resourceScopes[resourceID]
	if !ok {
		notFound(w)
		return
	}
	for _, sc := range scopes {
		if str(sc, "name") == str(body, "name") {
			conflict(w, "scope.name_exists")
			return
		}
	}
	id := s.nextID("sc")
	o := object{"tenantId": "default", "id": id, "resourceId": resourceID, "name": str(body, "name"), "description": body["description"], "createdAt": now()}
	scopes[id] = o
	writeJSON(w, http.StatusCreated, o)
}

// UpdateResourceScope is PATCH /api/resources/{resourceId}/scopes/{scopeId}.
func (s *Server) UpdateResourceScope(w http.ResponseWriter, r *http.Request, resourceID mgmt.ResourceId, scopeID mgmt.ScopeId) {
	body, ok := decode(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initResources()
	o, ok := s.resourceScopes[resourceID][scopeID]
	if !ok {
		notFound(w)
		return
	}
	merge(o, body)
	writeJSON(w, http.StatusOK, o)
}

// DeleteResourceScope is DELETE /api/resources/{resourceId}/scopes/{scopeId}.
func (s *Server) DeleteResourceScope(w http.ResponseWriter, _ *http.Request, resourceID mgmt.ResourceId, scopeID mgmt.ScopeId) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initResources()
	if _, ok := s.resourceScopes[resourceID][scopeID]; !ok {
		notFound(w)
		return
	}
	delete(s.resourceScopes[resourceID], scopeID)
	s.dropRoleScopes(scopeID)
	w.WriteHeader(http.StatusNoContent)
}

// scopeByID finds a resource scope anywhere.
func (s *Server) scopeByID(id string) (object, bool) {
	s.initResources()
	for _, scopes := range s.resourceScopes {
		if o, ok := scopes[id]; ok {
			return o, true
		}
	}
	return nil, false
}
