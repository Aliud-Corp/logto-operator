package logtofake

import (
	"net/http"

	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

// SMTPFactory is the factory id of Logto's SMTP connector.
const SMTPFactory = "simple-mail-transfer-protocol"

func (s *Server) initConnectors() {
	if s.connectors == nil {
		s.connectors = map[string]object{}
	}
}

// Connectors returns the stored connectors by id (copies).
func (s *Server) Connectors() map[string]map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initConnectors()
	out := map[string]map[string]any{}
	for id, o := range s.connectors {
		out[id] = copyObject(o)
	}
	return out
}

// smtpIssues validates an SMTP config as Logto 1.44-1.46 does (measured): host, port (a number), auth and fromEmail
// required, and the Register, SignIn, ForgotPassword and Generic templates.
func smtpIssues(config map[string]any) []object {
	var issues []object
	for _, k := range []string{"host", "fromEmail"} {
		if str(config, k) == "" {
			issues = append(issues, object{"message": "Required", "path": []any{k}})
		}
	}
	if _, ok := config["port"].(float64); !ok {
		issues = append(issues, object{"message": "Required", "path": []any{"port"}})
	}
	if auth, ok := config["auth"].(map[string]any); !ok || str(auth, "user") == "" || str(auth, "pass") == "" {
		issues = append(issues, object{"message": "Invalid input", "path": []any{"auth"}})
	}
	have := map[string]bool{}
	if list, ok := config["templates"].([]any); ok {
		for _, t := range list {
			if m, ok := t.(map[string]any); ok {
				have[str(m, "usageType")] = true
			}
		}
	}
	for _, u := range []string{"Register", "SignIn", "ForgotPassword", "Generic"} {
		if !have[u] {
			issues = append(issues, object{"message": "Template with UsageType (" + u + ") should be provided!", "path": []any{"templates"}})
		}
	}
	return issues
}

func invalidConfig(w http.ResponseWriter, issues []object) {
	writeJSON(w, http.StatusBadRequest, object{
		"code": "connector.invalid_config", "message": "The connector's config is invalid.",
		"data": object{"issues": issues, "name": "ZodError"},
	})
}

func connectorView(o object) object {
	v := copyObject(o)
	for k, d := range map[string]any{
		"syncProfile": false, "enableTokenStorage": false, "metadata": object{}, "target": "smtp",
		"name": object{"en": "SMTP"}, "description": object{"en": "SMTP"}, "logo": "", "logoDark": nil,
		"readme": "", "platform": nil, "type": "Email",
	} {
		if _, ok := v[k]; !ok {
			v[k] = d
		}
	}
	return v
}

// ListConnectors is GET /api/connectors (not paginated).
func (s *Server) ListConnectors(w http.ResponseWriter, _ *http.Request, _ mgmt.ListConnectorsParams) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initConnectors()
	out := []object{}
	for _, o := range sorted(s.connectors) {
		out = append(out, connectorView(o))
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateConnector is POST /api/connectors; Logto accepts a declared id.
func (s *Server) CreateConnector(w http.ResponseWriter, r *http.Request) {
	body, ok := decode(w, r)
	if !ok {
		return
	}
	config, _ := body["config"].(map[string]any)
	if str(body, "connectorId") == SMTPFactory {
		if issues := smtpIssues(config); len(issues) > 0 {
			invalidConfig(w, issues)
			return
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initConnectors()
	id := str(body, "id")
	if id == "" {
		id = s.nextID("conn")
	}
	if _, exists := s.connectors[id]; exists {
		conflict(w, "connector.id_exists")
		return
	}
	o := object{"id": id, "connectorId": str(body, "connectorId"), "config": config}
	s.connectors[id] = o
	writeJSON(w, http.StatusOK, connectorView(o))
}

// GetConnector is GET /api/connectors/{id}: the config comes back whole, secrets in clear (measured).
func (s *Server) GetConnector(w http.ResponseWriter, _ *http.Request, id mgmt.ConnectorIdRoot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initConnectors()
	o, ok := s.connectors[id]
	if !ok {
		notFound(w)
		return
	}
	writeJSON(w, http.StatusOK, connectorView(o))
}

// UpdateConnector is PATCH /api/connectors/{id}: a config is validated whole and replaces the stored one (a partial
// config is refused, measured).
func (s *Server) UpdateConnector(w http.ResponseWriter, r *http.Request, id mgmt.ConnectorIdRoot) {
	body, ok := decode(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initConnectors()
	o, ok := s.connectors[id]
	if !ok {
		notFound(w)
		return
	}
	if config, has := body["config"].(map[string]any); has {
		if str(o, "connectorId") == SMTPFactory {
			if issues := smtpIssues(config); len(issues) > 0 {
				invalidConfig(w, issues)
				return
			}
		}
		o["config"] = config
	}
	writeJSON(w, http.StatusOK, connectorView(o))
}

// DeleteConnector is DELETE /api/connectors/{id}.
func (s *Server) DeleteConnector(w http.ResponseWriter, _ *http.Request, id mgmt.ConnectorIdRoot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.initConnectors()
	if _, ok := s.connectors[id]; !ok {
		notFound(w)
		return
	}
	delete(s.connectors, id)
	w.WriteHeader(http.StatusNoContent)
}
