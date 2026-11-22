//go:build integration

package integration_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/logto/client"
	"github.com/Aliud-Corp/logto-operator/internal/logto/integration"
	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

// The two facts the SMTP connector kind depends on (question Q5 of the plan), measured on Logto 1.44.0 and 1.46.0
// and asserted on every run, so a Logto version that changes either fails here first:
//
//  1. GET /api/connectors/{id} returns the config with auth.pass in clear: a changed password is drift the kind sees
//     by comparing, no hash or write-only handling needed.
//  2. PATCH /api/connectors/{id} validates the config it receives as a whole: a partial config is refused (400), so
//     the kind always sends the whole config, secrets included.
func TestTheSMTPConnectorBehavesAsTheKindAssumes(t *testing.T) {
	env := integration.Load(t)
	c := client.New(client.Config{Endpoint: env.Endpoint, TokenEndpoint: env.AdminEndpoint, ClientID: env.ClientID, ClientSecret: env.ClientSecret})
	ctx := context.Background()
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	id := "probe-smtp-" + hex.EncodeToString(b)
	pass := "probe-pass-" + hex.EncodeToString(b)

	tmpl := func(usage string) map[string]any {
		return map[string]any{"usageType": usage, "contentType": "text/plain", "subject": "s {{code}}", "content": "c {{code}}"}
	}
	config := map[string]any{
		"host": "smtp.example.test", "port": 587, "fromEmail": "no-reply@example.test",
		"auth":      map[string]any{"user": "probe", "pass": pass},
		"templates": []any{tmpl("Register"), tmpl("SignIn"), tmpl("ForgotPassword"), tmpl("Generic")},
	}
	body, _ := json.Marshal(map[string]any{"id": id, "connectorId": "simple-mail-transfer-protocol", "config": config})
	req, err := mgmt.NewCreateConnectorRequestWithBody(c.Endpoint(), "application/json", strings.NewReader(string(body)))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Do(ctx, req, nil); err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() {
		if req, err := mgmt.NewDeleteConnectorRequest(c.Endpoint(), id); err == nil {
			_ = c.Do(ctx, req, nil)
		}
	})

	get := func() map[string]any {
		t.Helper()
		req, err := mgmt.NewGetConnectorRequest(c.Endpoint(), id)
		if err != nil {
			t.Fatal(err)
		}
		var out struct {
			Config map[string]any `json:"config"`
		}
		if err := c.Do(ctx, req, &out); err != nil {
			t.Fatal(err)
		}
		return out.Config
	}

	got := get()
	auth, _ := got["auth"].(map[string]any)
	t.Logf("Logto %s: GET returns auth.pass in clear: %v", env.Version, auth["pass"] == pass)

	partial, _ := json.Marshal(map[string]any{"config": map[string]any{"templates": config["templates"]}})
	req, err = mgmt.NewUpdateConnectorRequestWithBody(c.Endpoint(), id, "application/json", strings.NewReader(string(partial)))
	if err != nil {
		t.Fatal(err)
	}
	patchErr := c.Do(ctx, req, nil)
	after := get()
	_, hostKept := after["host"]
	t.Logf("Logto %s: partial PATCH error: %v; host kept after a templates-only PATCH: %v", env.Version, patchErr, hostKept)

	if auth["pass"] != pass {
		t.Errorf("GET no longer returns auth.pass in clear: the connector kind's drift rule must change")
	}
	if patchErr == nil {
		t.Errorf("a partial config PATCH was accepted: the connector kind may stop re-sending the whole config")
	}
	if !hostKept {
		t.Errorf("the refused PATCH changed the stored config")
	}
}
