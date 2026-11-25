package logtofake

import (
	"net/http"

	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

// consent is what an application's consent screen may grant: resource scope ids and user scope names.
type consent struct {
	resources []string
	user      []string
}

// ConsentScopes are the user consent scopes of an application, for test assertions: resource scopes as
// "<indicator> <scope>", and user scopes by name.
type ConsentScopes struct {
	Resources []string
	User      []string
}

// ApplicationConsentScopes returns the consent scopes of an application.
func (s *Server) ApplicationConsentScopes(id string) ConsentScopes {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initApplications()
	s.initResources()
	out := ConsentScopes{}
	if c := s.appConsent[id]; c != nil {
		for _, sid := range c.resources {
			if sc, ok := s.scopeByID(sid); ok {
				out.Resources = append(out.Resources, str(s.resources[str(sc, "resourceId")], "indicator")+" "+str(sc, "name"))
			}
		}
		out.User = append(out.User, c.user...)
	}
	return out
}

// ListApplicationUserConsentScopes is GET /api/applications/{applicationId}/user-consent-scopes: the resource scopes
// are grouped by resource, as Logto answers them. Organization scopes are never held.
func (s *Server) ListApplicationUserConsentScopes(w http.ResponseWriter, _ *http.Request, id mgmt.ApplicationId) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initApplications()
	s.initResources()
	if _, ok := s.apps[id]; !ok {
		notFound(w)
		return
	}
	byResource := map[string][]any{}
	var order []string
	user := []string{}
	if c := s.appConsent[id]; c != nil {
		for _, sid := range c.resources {
			sc, ok := s.scopeByID(sid)
			if !ok {
				continue
			}
			rid := str(sc, "resourceId")
			if _, seen := byResource[rid]; !seen {
				order = append(order, rid)
			}
			byResource[rid] = append(byResource[rid], object{"id": sid, "name": str(sc, "name"), "description": sc["description"]})
		}
		user = append(user, c.user...)
	}
	groups := []object{}
	for _, rid := range order {
		res := s.resources[rid]
		groups = append(groups, object{
			"resource": object{"id": rid, "name": str(res, "name"), "indicator": str(res, "indicator")},
			"scopes":   byResource[rid],
		})
	}
	writeJSON(w, http.StatusOK, object{
		"organizationScopes": []any{}, "resourceScopes": groups, "organizationResourceScopes": []any{}, "userScopes": user,
	})
}

// CreateApplicationUserConsentScope is POST /api/applications/{applicationId}/user-consent-scopes: the scopes are
// added. Only a third-party application has a consent screen (422, like Logto); an unknown resource scope answers 404.
func (s *Server) CreateApplicationUserConsentScope(w http.ResponseWriter, r *http.Request, id mgmt.ApplicationId) {
	body, ok := decode(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initApplications()
	s.initResources()
	app, ok := s.apps[id]
	if !ok {
		notFound(w)
		return
	}
	if third, _ := app["isThirdParty"].(bool); !third {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "application.third_party_application_only", "message": "Only third-party applications have consent scopes."})
		return
	}
	add := stringList(body, "resourceScopes")
	for _, sid := range add {
		if _, ok := s.scopeByID(sid); !ok {
			notFound(w)
			return
		}
	}
	c := s.appConsent[id]
	if c == nil {
		c = &consent{}
		s.appConsent[id] = c
	}
	for _, sid := range add {
		if !contains(c.resources, sid) {
			c.resources = append(c.resources, sid)
		}
	}
	for _, u := range stringList(body, "userScopes") {
		if !contains(c.user, u) {
			c.user = append(c.user, u)
		}
	}
	w.WriteHeader(http.StatusCreated)
}

// DeleteApplicationUserConsentScope is DELETE /api/applications/{applicationId}/user-consent-scopes/{scopeType}/{scopeId}:
// the scope id of a resource scope, the name of a user scope. A scope the application does not hold answers 404.
func (s *Server) DeleteApplicationUserConsentScope(w http.ResponseWriter, _ *http.Request, id mgmt.ApplicationId, scopeType mgmt.DeleteApplicationUserConsentScopeParamsScopeType, scopeID mgmt.ScopeId) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initApplications()
	if _, ok := s.apps[id]; !ok {
		notFound(w)
		return
	}
	c := s.appConsent[id]
	if c == nil {
		notFound(w)
		return
	}
	switch string(scopeType) {
	case "resource-scopes":
		if !contains(c.resources, scopeID) {
			notFound(w)
			return
		}
		c.resources = without(c.resources, scopeID)
	case "user-scopes":
		if !contains(c.user, scopeID) {
			notFound(w)
			return
		}
		c.user = without(c.user, scopeID)
	default:
		notFound(w)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
