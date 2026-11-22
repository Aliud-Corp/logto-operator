package logtofake

import (
	"net/http"
	"strings"

	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

func (s *Server) initUsers() {
	if s.users == nil {
		s.users = map[string]object{}
		s.userRoles = map[string][]string{}
	}
}

// User is a stored user as a test reads it.
type User struct {
	ID, PrimaryEmail, Name string
	HasPassword            bool
	Roles                  []string
}

// Users returns the stored users by primary e-mail.
func (s *Server) Users() map[string]User {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initUsers()
	s.initRoles()
	out := map[string]User{}
	for id, o := range s.users {
		u := User{ID: id, PrimaryEmail: str(o, "primaryEmail"), Name: str(o, "name"), HasPassword: o["passwordSet"] == true}
		for _, rid := range s.userRoles[id] {
			u.Roles = append(u.Roles, str(s.roles.rows[rid], "name"))
		}
		out[u.PrimaryEmail] = u
	}
	return out
}

func userView(o object) object {
	v := copyObject(o)
	delete(v, "passwordSet")
	return v
}

// ListUsers is GET /api/users, paginated, with Logto's search on primaryEmail: mode.primaryEmail=exact matches the
// value as is, the default mode matches it as a case-insensitive substring.
func (s *Server) ListUsers(w http.ResponseWriter, r *http.Request, _ mgmt.ListUsersParams) {
	q := r.URL.Query()
	want, exact := q.Get("search.primaryEmail"), q.Get("mode.primaryEmail") == "exact"
	s.mu.Lock()
	s.initUsers()
	items := []object{}
	for _, o := range sorted(s.users) {
		email := str(o, "primaryEmail")
		if want != "" && (exact && email != want || !exact && !strings.Contains(strings.ToLower(email), strings.ToLower(want))) {
			continue
		}
		items = append(items, userView(o))
	}
	s.mu.Unlock()
	paginate(w, r, items)
}

// CreateUser is POST /api/users; an e-mail in use answers 422.
func (s *Server) CreateUser(w http.ResponseWriter, r *http.Request) {
	body, ok := decode(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initUsers()
	for _, o := range s.users {
		if str(o, "primaryEmail") != "" && str(o, "primaryEmail") == str(body, "primaryEmail") {
			conflict(w, "user.email_already_in_use")
			return
		}
	}
	id := s.nextID("user")
	o := object{
		"id": id, "username": nil, "primaryEmail": body["primaryEmail"], "primaryPhone": nil, "name": body["name"], "avatar": nil,
		"customData": object{}, "identities": object{}, "lastSignInAt": nil, "createdAt": now(), "updatedAt": now(),
		"profile": object{}, "applicationId": nil, "cimdClientId": nil, "isSuspended": false, "passwordSet": body["password"] != nil,
	}
	s.users[id] = o
	writeJSON(w, http.StatusOK, userView(o))
}

// UpdateUser is PATCH /api/users/{userId}.
func (s *Server) UpdateUser(w http.ResponseWriter, r *http.Request, id mgmt.UserId) {
	body, ok := decode(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initUsers()
	o, ok := s.users[id]
	if !ok {
		notFound(w)
		return
	}
	merge(o, body)
	writeJSON(w, http.StatusOK, userView(o))
}

// ListUserRoles is GET /api/users/{userId}/roles.
func (s *Server) ListUserRoles(w http.ResponseWriter, r *http.Request, id mgmt.UserId, _ mgmt.ListUserRolesParams) {
	s.mu.Lock()
	s.initUsers()
	s.initRoles()
	items := []object{}
	for _, rid := range s.userRoles[id] {
		items = append(items, s.roles.rows[rid])
	}
	s.mu.Unlock()
	paginate(w, r, items)
}

// AssignUserRoles is POST /api/users/{userId}/roles: the roles are added.
func (s *Server) AssignUserRoles(w http.ResponseWriter, r *http.Request, id mgmt.UserId) {
	body, ok := decode(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initUsers()
	s.initRoles()
	if _, ok := s.users[id]; !ok {
		notFound(w)
		return
	}
	for _, rid := range stringList(body, "roleIds") {
		if !contains(s.userRoles[id], rid) {
			s.userRoles[id] = append(s.userRoles[id], rid)
		}
	}
	w.WriteHeader(http.StatusCreated)
}
