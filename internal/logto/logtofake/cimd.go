package logtofake

import (
	"net/http"

	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

// The tenant's Client ID Metadata Document setting: its config and the consent ceiling of its clients. Logto's own
// default is off, with no consent scope.
func (s *Server) initCIMD() {
	if s.cimd != nil {
		return
	}
	s.cimd = object{"enabled": false}
	s.cimdConsent = &consent{}
}

// CIMD returns the stored CIMD config (a copy).
func (s *Server) CIMD() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initCIMD()
	return copyObject(s.cimd)
}

// CIMDConsentScopes returns the consent scopes of the tenant's CIMD clients.
func (s *Server) CIMDConsentScopes() ConsentScopes {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initCIMD()
	s.initResources()
	return s.scopes(s.cimdConsent)
}

// BlockCIMD makes the fake refuse to change the CIMD config, as Logto does while its outbound SSRF protection is
// disabled or restricted by an allowlist.
func (s *Server) BlockCIMD() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cimdBlocked = true
}

// GetCimdConfig is GET /api/configs/cimd.
func (s *Server) GetCimdConfig(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initCIMD()
	writeJSON(w, http.StatusOK, s.cimd)
}

// UpdateCimdConfig is PATCH /api/configs/cimd: the keys sent replace the stored ones.
func (s *Server) UpdateCimdConfig(w http.ResponseWriter, r *http.Request) {
	body, ok := decode(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initCIMD()
	if s.cimdBlocked {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": "cimd.ssrf_protection_required", "message": "CIMD requires SSRF protection to be enabled without an allowlist."})
		return
	}
	for _, k := range []string{"enabled", "addConsentPromptForOfflineAccess"} {
		if v, ok := body[k]; ok {
			s.cimd[k] = v
		}
	}
	writeJSON(w, http.StatusOK, s.cimd)
}

// ListCimdUserConsentScopes is GET /api/cimd/user-consent-scopes.
func (s *Server) ListCimdUserConsentScopes(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initCIMD()
	s.initResources()
	writeJSON(w, http.StatusOK, s.listing(s.cimdConsent))
}

// AssignCimdUserConsentScopes is POST /api/cimd/user-consent-scopes: the scopes are added.
func (s *Server) AssignCimdUserConsentScopes(w http.ResponseWriter, r *http.Request) {
	body, ok := decode(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initCIMD()
	s.initResources()
	if s.add(w, body, s.cimdConsent) {
		w.WriteHeader(http.StatusCreated)
	}
}

// DeleteCimdUserConsentScope is DELETE /api/cimd/user-consent-scopes/{scopeType}/{scopeId}.
func (s *Server) DeleteCimdUserConsentScope(w http.ResponseWriter, _ *http.Request, scopeType mgmt.DeleteCimdUserConsentScopeParamsScopeType, scopeID mgmt.ScopeId) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initCIMD()
	remove(w, s.cimdConsent, string(scopeType), scopeID)
}
