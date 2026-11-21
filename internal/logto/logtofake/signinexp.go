package logtofake

import (
	"net/http"

	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

func (s *Server) initSignInExp() {
	if s.signInExp != nil {
		return
	}
	// Logto's defaults, for the keys the engine manages.
	s.signInExp = object{
		"tenantId": "default", "id": "default",
		"color":                     object{"primaryColor": "#6139F6", "isDarkModeEnabled": false, "darkPrimaryColor": "#8C6CFF"},
		"languageInfo":              object{"autoDetect": true, "fallbackLanguage": "en"},
		"branding":                  object{},
		"customCss":                 nil,
		"signUp":                    object{"identifiers": []any{"username"}, "password": true, "verify": false},
		"signIn":                    object{"methods": []any{object{"identifier": "username", "password": true, "verificationCode": false, "isPasswordPrimary": true}}},
		"unknownSessionRedirectUrl": nil,
		"signInMode":                "SignInAndRegister",
	}
}

// SignInExperience returns the stored sign-in experience (a copy).
func (s *Server) SignInExperience() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initSignInExp()
	return copyObject(s.signInExp)
}

// GetSignInExp is GET /api/sign-in-exp.
func (s *Server) GetSignInExp(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initSignInExp()
	writeJSON(w, http.StatusOK, s.signInExp)
}

// UpdateSignInExp is PATCH /api/sign-in-exp: top-level keys replace the stored ones. An e-mail sign-up identifier
// with verification is refused while no e-mail connector exists, as Logto does.
func (s *Server) UpdateSignInExp(w http.ResponseWriter, r *http.Request, _ mgmt.UpdateSignInExpParams) {
	body, ok := decode(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initSignInExp()
	s.initConnectors()
	if su, ok := body["signUp"].(map[string]any); ok && su["verify"] == true {
		for _, id := range stringList(su, "identifiers") {
			if id == "email" && !s.hasEmailConnector() {
				writeJSON(w, http.StatusBadRequest, map[string]string{"code": "sign_in_experiences.enabled_connector_not_found", "message": "Enabled email connector not found."})
				return
			}
		}
	}
	merge(s.signInExp, body)
	writeJSON(w, http.StatusOK, s.signInExp)
}

func (s *Server) hasEmailConnector() bool {
	for _, c := range s.connectors {
		if str(c, "connectorId") == SMTPFactory {
			return true
		}
	}
	return false
}
