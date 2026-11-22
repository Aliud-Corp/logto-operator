package outputs_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/engine"
	"github.com/Aliud-Corp/logto-operator/internal/outputs"
	"github.com/Aliud-Corp/logto-operator/internal/secrets"
)

const secretValue = "m2m-secret-value"

func sample() *engine.Outputs {
	o := &engine.Outputs{}
	o.SetID("applications.backend.id", "app123")
	o.SetID("applications.app.id", "app456")
	o.SetSecret("applications.backend.secret", secrets.New(secretValue))
	return o
}

func TestIDsAreWrittenSortedAndWithoutSecrets(t *testing.T) {
	var env, js bytes.Buffer
	if err := outputs.WriteIDs(&env, sample(), "env"); err != nil {
		t.Fatal(err)
	}
	if env.String() != "applications.app.id=app456\napplications.backend.id=app123\n" {
		t.Fatalf("env = %q", env.String())
	}
	if err := outputs.WriteIDs(&js, sample(), "json"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(env.String()+js.String(), secretValue) {
		t.Fatal("a secret is written with the ids")
	}
}

func TestSecretsGoToA0600FileEvenWhenItExisted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := outputs.WriteSecretsFile(sample(), path); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, want 0600", st.Mode().Perm())
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), `"applications.backend.secret": "`+secretValue+`"`) {
		t.Fatalf("file = %s", b)
	}
}

func TestSecretsGoToAnInheritedDescriptor(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := outputs.WriteSecretsFD(sample(), int(w.Fd())); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	var got bytes.Buffer
	_, _ = got.ReadFrom(r)
	if !strings.Contains(got.String(), secretValue) {
		t.Fatalf("descriptor got %q", got.String())
	}
	if err := outputs.WriteSecretsFD(sample(), 1); err == nil {
		t.Fatal("stdout must be refused as a secrets descriptor")
	}
}
