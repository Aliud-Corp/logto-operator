package connectors_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/connectors"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindtest"
	"github.com/Aliud-Corp/logto-operator/internal/engine/plan"
	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

const declared = `  connectors:
    smtp:
      id: example-smtp
      connectorId: simple-mail-transfer-protocol
      smtp:
        host: smtp.example.test
        port: 587
        auth: {user: {value: mailer}, pass: {valueFrom: {env: TEST_SMTP_PASS}}}
        fromEmail: no-reply@example.test
        templates:
          items:
            - {usageType: Register, contentType: text/plain, subject: "Register {{code}}", content: {value: "c {{code}}"}}
            - {usageType: SignIn, contentType: text/plain, subject: "Sign in {{code}}", content: {value: "c {{code}}"}}
            - {usageType: ForgotPassword, contentType: text/plain, subject: "Reset {{code}}", content: {value: "c {{code}}"}}
            - {usageType: Generic, contentType: text/plain, subject: "Code {{code}}", content: {value: "c {{code}}"}}
`

func TestTheConnectorIsCreatedAndAReplayWritesNothing(t *testing.T) {
	t.Setenv("TEST_SMTP_PASS", "first-password")
	fake, c := kindtest.Fake(t)
	spec := kindtest.Spec(t, declared)
	kindtest.Apply(t, c, spec, connectors.Kind{})
	conn, ok := fake.Connectors()["example-smtp"]
	if !ok {
		t.Fatalf("connectors = %v", fake.Connectors())
	}
	cfg := conn["config"].(map[string]any)
	if cfg["host"] != "smtp.example.test" || len(cfg["templates"].([]any)) != 4 {
		t.Fatalf("config = %v", cfg)
	}
	writes := fake.Writes()
	kindtest.Unchanged(t, kindtest.Apply(t, c, spec, connectors.Kind{}))
	if fake.Writes() != writes {
		t.Fatalf("the replay wrote %d times", fake.Writes()-writes)
	}
}

func TestAChangedPasswordIsDriftShownMasked(t *testing.T) {
	t.Setenv("TEST_SMTP_PASS", "first-password")
	fake, c := kindtest.Fake(t)
	kindtest.Apply(t, c, kindtest.Spec(t, declared), connectors.Kind{})
	t.Setenv("TEST_SMTP_PASS", "second-password")
	p := kindtest.Apply(t, c, kindtest.Spec(t, declared), connectors.Kind{})
	var out bytes.Buffer
	_ = plan.Render(&out, p)
	if !strings.Contains(out.String(), "config.auth.pass: (sensitive) -> (sensitive)") || strings.Contains(out.String(), "password") {
		t.Fatalf("plan:\n%s", out.String())
	}
	cfg := fake.Connectors()["example-smtp"]["config"].(map[string]any)
	if cfg["auth"].(map[string]any)["pass"] != "second-password" || cfg["host"] != "smtp.example.test" {
		t.Fatalf("the whole config was not sent: %v", cfg)
	}
}

func TestAnUndeclaredIDIsAdoptedWhenItIsTheOnlyOneOfItsFactory(t *testing.T) {
	t.Setenv("TEST_SMTP_PASS", "first-password")
	fake, c := kindtest.Fake(t)
	kindtest.Apply(t, c, kindtest.Spec(t, declared), connectors.Kind{})
	withoutID := strings.Replace(declared, "      id: example-smtp\n", "", 1)
	kindtest.Unchanged(t, kindtest.Apply(t, c, kindtest.Spec(t, withoutID), connectors.Kind{}))
	if n := len(fake.Connectors()); n != 1 {
		t.Fatalf("%d connectors, want 1 (adopted)", n)
	}
}

func TestAMissingPasswordStopsThePlan(t *testing.T) {
	_, c := kindtest.Fake(t)
	_, err := kindtest.Plan(c, kindtest.Spec(t, declared), connectors.Kind{})
	if err == nil || !strings.Contains(err.Error(), "spec.connectors.smtp.smtp.auth.pass.valueFrom: environment variable TEST_SMTP_PASS is unset or empty") {
		t.Fatalf("err = %v", err)
	}
}

func TestPruneRemovesOnlyUndeclaredSMTPConnectorsNotKept(t *testing.T) {
	t.Setenv("TEST_SMTP_PASS", "first-password")
	fake, c := kindtest.Fake(t)
	kindtest.Apply(t, c, kindtest.Spec(t, declared), connectors.Kind{})
	smtp := fake.Connectors()["example-smtp"]["config"]
	for id, factory := range map[string]string{"stray-smtp": connectors.SMTPFactory, "kept-smtp": connectors.SMTPFactory, "social": "github-universal"} {
		body, _ := json.Marshal(map[string]any{"id": id, "connectorId": factory, "config": smtp})
		req, _ := mgmt.NewCreateConnectorRequestWithBody(c.Endpoint(), "application/json", bytes.NewReader(body))
		if err := c.Do(context.Background(), req, nil); err != nil {
			t.Fatal(err)
		}
	}
	kindtest.Apply(t, c, kindtest.Spec(t, declared), connectors.Kind{})
	if n := len(fake.Connectors()); n != 4 {
		t.Fatalf("%d connectors without prune, want 4", n)
	}
	kindtest.Apply(t, c, kindtest.Spec(t, "  prune: {connectors: true, keep: [kept-smtp]}\n"+declared), connectors.Kind{})
	have := fake.Connectors()
	for id, want := range map[string]bool{"example-smtp": true, "kept-smtp": true, "social": true, "stray-smtp": false} {
		if _, ok := have[id]; ok != want {
			t.Errorf("connector %s present = %v, want %v", id, ok, want)
		}
	}
}
