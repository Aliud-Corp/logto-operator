package logtofake

import (
	"net/http"
)

func (s *Server) initProfileFields() {
	if s.profileFields == nil {
		s.profileFields = map[string]object{}
	}
}

// ProfileFields returns the stored custom profile fields by name (copies).
func (s *Server) ProfileFields() map[string]map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initProfileFields()
	out := map[string]map[string]any{}
	for name, o := range s.profileFields {
		out[name] = copyObject(o)
	}
	return out
}

func (s *Server) profileFieldList() []object {
	out := sorted(s.profileFields)
	if out == nil {
		out = []object{}
	}
	return out
}

// ListCustomProfileFields is GET /api/custom-profile-fields (not paginated in Logto).
func (s *Server) ListCustomProfileFields(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initProfileFields()
	writeJSON(w, http.StatusOK, s.profileFieldList())
}

// CreateCustomProfileField is POST /api/custom-profile-fields; a name in use answers 422.
func (s *Server) CreateCustomProfileField(w http.ResponseWriter, r *http.Request) {
	body, ok := decode(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initProfileFields()
	name := str(body, "name")
	if _, exists := s.profileFields[name]; exists {
		conflict(w, "custom_profile_fields.name_exists")
		return
	}
	o := object{
		"tenantId": "default", "id": s.nextID("pf"), "createdAt": now(), "sieOrder": len(s.profileFields) + 1,
		"label": "", "description": nil, "config": object{},
	}
	merge(o, body)
	s.profileFields[name] = o
	writeJSON(w, http.StatusCreated, o)
}

// UpdateCustomProfileFieldByName is PUT /api/custom-profile-fields/{name}: the body (without the name) replaces it.
func (s *Server) UpdateCustomProfileFieldByName(w http.ResponseWriter, r *http.Request, name string) {
	body, ok := decode(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initProfileFields()
	old, exists := s.profileFields[name]
	if !exists {
		notFound(w)
		return
	}
	o := object{
		"tenantId": "default", "id": old["id"], "createdAt": old["createdAt"], "sieOrder": old["sieOrder"], "name": name,
		"label": "", "description": nil, "config": object{},
	}
	merge(o, body)
	s.profileFields[name] = o
	writeJSON(w, http.StatusOK, o)
}

// DeleteCustomProfileFieldByName is DELETE /api/custom-profile-fields/{name}.
func (s *Server) DeleteCustomProfileFieldByName(w http.ResponseWriter, _ *http.Request, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initProfileFields()
	if _, exists := s.profileFields[name]; !exists {
		notFound(w)
		return
	}
	delete(s.profileFields, name)
	w.WriteHeader(http.StatusNoContent)
}
