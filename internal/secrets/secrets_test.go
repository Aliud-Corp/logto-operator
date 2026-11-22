package secrets_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Aliud-Corp/logto-operator/api/v1alpha1"
	"github.com/Aliud-Corp/logto-operator/internal/secrets"
)

const value = "a-very-secret-value"

func TestASecretNeverPrintsItsValue(t *testing.T) {
	s := secrets.New(value)
	wrapped := struct {
		Pass secrets.Secret `json:"pass"`
	}{s}
	js, err := json.Marshal(wrapped)
	if err != nil {
		t.Fatal(err)
	}
	for name, out := range map[string]string{
		"%v": fmt.Sprintf("%v", s), "%s": fmt.Sprintf("%s", s), "%+v": fmt.Sprintf("%+v", wrapped),
		"%#v": fmt.Sprintf("%#v", s), "%q": fmt.Sprintf("%q", s), "json": string(js),
	} {
		if strings.Contains(out, value) {
			t.Errorf("%s leaks the value: %s", name, out)
		}
	}
	if s.Reveal() != value {
		t.Fatal("Reveal must return the value")
	}
}

func resolver(env map[string]string) secrets.Resolver {
	return secrets.Resolver{
		Getenv:   func(k string) (string, bool) { v, ok := env[k]; return v, ok },
		ReadFile: os.ReadFile,
		Redactor: &secrets.Redactor{},
	}
}

func TestEnvAndFileSourcesResolve(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pass")
	if err := os.WriteFile(path, []byte(value+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := resolver(map[string]string{"PASS": value})
	for _, src := range []v1alpha1.ValueFrom{{Env: "PASS"}, {File: path}} {
		s, err := r.Secret("p", v1alpha1.SecretSource{ValueFrom: src})
		if err != nil {
			t.Fatal(err)
		}
		if s.Reveal() != value {
			t.Fatalf("%+v resolved to another value", src)
		}
	}
}

func TestAnUnsetVariableIsAnErrorNamingThePath(t *testing.T) {
	_, err := resolver(nil).Secret("spec.connectors.smtp.smtp.auth.pass", v1alpha1.SecretSource{ValueFrom: v1alpha1.ValueFrom{Env: "NOPE"}})
	if err == nil || !strings.Contains(err.Error(), "spec.connectors.smtp.smtp.auth.pass.valueFrom: environment variable NOPE is unset or empty") {
		t.Fatalf("err = %v", err)
	}
}

func TestTheCLIRefusesASecretKeyRef(t *testing.T) {
	_, err := secrets.CLI(&secrets.Redactor{}).Secret("p", v1alpha1.SecretSource{ValueFrom: v1alpha1.ValueFrom{SecretKeyRef: &v1alpha1.SecretKeyRef{Name: "s", Key: "k"}}})
	if err == nil || !strings.Contains(err.Error(), "read by the operator only") {
		t.Fatalf("err = %v", err)
	}
}

func TestTheRedactorRemovesResolvedSecretsFromText(t *testing.T) {
	r := resolver(map[string]string{"PASS": value})
	if _, err := r.Secret("p", v1alpha1.SecretSource{ValueFrom: v1alpha1.ValueFrom{Env: "PASS"}}); err != nil {
		t.Fatal(err)
	}
	err := errors.New("Logto answered 400: invalid config, auth.pass=" + value)
	if got := r.Redactor.Redact(err.Error()); strings.Contains(got, value) || !strings.Contains(got, secrets.Masked) {
		t.Fatalf("redacted = %q", got)
	}
}

// The operator reads a secretKeyRef through SecretKey; the value is registered for redaction, an empty one refused.
func TestASecretKeyRefIsReadThroughSecretKey(t *testing.T) {
	ref := v1alpha1.SecretSource{ValueFrom: v1alpha1.ValueFrom{SecretKeyRef: &v1alpha1.SecretKeyRef{Name: "logto", Key: "pass"}}}
	red := &secrets.Redactor{}
	r := secrets.Resolver{Redactor: red, SecretKey: func(name, key string) (string, error) {
		if name == "logto" && key == "pass" {
			return "from-the-secret", nil
		}
		return "", nil
	}}
	got, err := r.Secret("spec.x", ref)
	if err != nil || got.Reveal() != "from-the-secret" {
		t.Fatalf("got %q, %v", got.Reveal(), err)
	}
	if out := red.Redact("leaked from-the-secret"); strings.Contains(out, "from-the-secret") {
		t.Fatalf("not redacted: %s", out)
	}
	ref.ValueFrom.SecretKeyRef.Key = "other"
	if _, err := r.Secret("spec.x", ref); err == nil || !strings.Contains(err.Error(), "key other of Secret logto is empty") {
		t.Fatalf("err = %v", err)
	}
}
