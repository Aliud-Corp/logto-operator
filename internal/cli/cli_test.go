package cli_test

import (
	"bytes"
	"context"
	"io/fs"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	logtospecs "github.com/Aliud-Corp/logto-operator/api/logto"
	"github.com/Aliud-Corp/logto-operator/internal/cli"
	"github.com/Aliud-Corp/logto-operator/internal/logto/client"
	"github.com/Aliud-Corp/logto-operator/internal/logto/contract"
	"github.com/Aliud-Corp/logto-operator/internal/logto/logtofake"
	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

const (
	clientSecret = "boot-secret-value"
	smtpPassword = "smtp-password-value"
)

// setup starts the fake (serving the newest vendored spec as its own) and writes the reference document, pointed
// at it, into a temporary directory with the files it references.
func setup(t *testing.T) (*logtofake.Server, *client.Client, string) {
	t.Helper()
	fake := logtofake.New("boot", clientSecret)
	versions := logtospecs.Versions()
	raw, err := logtospecs.Spec(versions[0])
	if err != nil {
		t.Fatal(err)
	}
	fake.ServeSpec(raw)
	docs, err := contract.Docs()
	if err != nil {
		t.Fatal(err)
	}
	fake.ValidateAgainst(docs)
	t.Cleanup(func() {
		for _, p := range fake.Problems() {
			if !strings.Contains(p, "fake.injected") {
				t.Errorf("not valid for every supported Logto: %s", p)
			}
		}
	})
	srv := httptest.NewServer(fake.Handler())
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	ref, err := os.ReadFile("../../examples/reference.yaml")
	if err != nil {
		t.Fatal(err)
	}
	doc := strings.NewReplacer("endpoint: http://localhost:3001", "endpoint: "+srv.URL, "tokenEndpoint: http://localhost:3002", "tokenEndpoint: "+srv.URL).Replace(string(ref))
	if err := os.WriteFile(filepath.Join(dir, "reference.yaml"), []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	copyDir(t, "../../examples/reference", filepath.Join(dir, "reference"))
	t.Setenv("LOGTO_CLIENT_ID", "boot")
	t.Setenv("LOGTO_CLIENT_SECRET", clientSecret)
	t.Setenv("EXAMPLE_SMTP_PASSWORD", smtpPassword)
	return fake, client.New(client.Config{Endpoint: srv.URL, ClientID: "boot", ClientSecret: clientSecret}), filepath.Join(dir, "reference.yaml")
}

func copyDir(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(to, rel), 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(to, rel), b, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
}

type result struct {
	code           int
	stdout, stderr string
}

func run(args ...string) result {
	var out, errb bytes.Buffer
	code := cli.Run(context.Background(), args, "test", &out, &errb)
	return result{code, out.String(), errb.String()}
}

func noLeak(t *testing.T, r result, values ...string) {
	t.Helper()
	for _, v := range values {
		if v != "" && strings.Contains(r.stdout+r.stderr, v) {
			t.Fatalf("a secret leaked into the output:\n--- stdout\n%s\n--- stderr\n%s", r.stdout, r.stderr)
		}
	}
}

func TestPlanApplyCheckOnTheReferenceDocument(t *testing.T) {
	fake, c, doc := setup(t)
	secretsFile := filepath.Join(t.TempDir(), "secrets.json")

	p := run("plan", "-f", doc)
	if p.code != cli.ExitOK || !strings.Contains(p.stdout, "to create") {
		t.Fatalf("plan: %d\n%s%s", p.code, p.stdout, p.stderr)
	}
	a := run("apply", "-f", doc, "--secrets-out", secretsFile)
	if a.code != cli.ExitOK {
		t.Fatalf("apply: %d\n%s%s", a.code, a.stdout, a.stderr)
	}
	if !strings.Contains(a.stdout, "applications.backend.id=") || !strings.Contains(a.stdout, "resources.app.indicator=https://app.example.test/api") {
		t.Fatalf("apply stdout:\n%s", a.stdout)
	}
	st, err := os.Stat(secretsFile)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("secrets file: %v %v", st, err)
	}
	b, _ := os.ReadFile(secretsFile)
	m2mSecret := ""
	for _, s := range fake.Applications() {
		if s["name"] == "Example Backend" {
			m2mSecret = fake.ApplicationSecrets(s["id"].(string))[0]
		}
	}
	if !strings.Contains(string(b), m2mSecret) {
		t.Fatalf("secrets file = %s", b)
	}
	noLeak(t, p, clientSecret, smtpPassword, m2mSecret)
	noLeak(t, a, clientSecret, smtpPassword, m2mSecret)

	writes := fake.Writes()
	again := run("apply", "-f", doc, "--secrets-out", secretsFile)
	if again.code != cli.ExitOK || fake.Writes() != writes {
		t.Fatalf("replay: code %d, %d writes", again.code, fake.Writes()-writes)
	}
	if ch := run("check", "-f", doc); ch.code != cli.ExitOK {
		t.Fatalf("check after apply: %d\n%s", ch.code, ch.stdout)
	}

	var appID string
	for _, s := range fake.Applications() {
		if s["name"] == "Example App" {
			appID = s["id"].(string)
		}
	}
	req, _ := mgmt.NewUpdateApplicationRequestWithBody(c.Endpoint(), appID, "application/json",
		strings.NewReader(`{"oidcClientMetadata":{"redirectUris":["https://evil.example/callback"],"postLogoutRedirectUris":[]}}`))
	if err := c.Do(context.Background(), req, nil); err != nil {
		t.Fatal(err)
	}
	ch := run("check", "-f", doc)
	if ch.code != cli.ExitDrift || !strings.Contains(ch.stdout, "oidcClientMetadata.redirectUris") {
		t.Fatalf("check after a hand edit: %d\n%s", ch.code, ch.stdout)
	}
	noLeak(t, ch, clientSecret, smtpPassword, m2mSecret)
}

func TestRenderShowsTheDocumentWithoutSecrets(t *testing.T) {
	_, _, doc := setup(t)
	r := run("render", "-f", doc)
	if r.code != cli.ExitOK || !strings.Contains(r.stdout, "data:image/svg+xml;base64,") || !strings.Contains(r.stdout, "env: EXAMPLE_SMTP_PASSWORD") {
		t.Fatalf("render: %d\n%s%s", r.code, r.stdout, r.stderr)
	}
	noLeak(t, r, clientSecret, smtpPassword)
}

func TestAWrongCredentialFailsClearlyWithoutTheSecret(t *testing.T) {
	_, _, doc := setup(t)
	t.Setenv("LOGTO_CLIENT_SECRET", "wrong-secret-value")
	r := run("plan", "-f", doc)
	if r.code != cli.ExitError || !strings.Contains(r.stderr, "check the credentials") {
		t.Fatalf("plan: %d\n%s", r.code, r.stderr)
	}
	noLeak(t, r, "wrong-secret-value", smtpPassword)
}

func TestPublishedSecretsNotWrittenAreAWarning(t *testing.T) {
	_, _, doc := setup(t)
	r := run("apply", "-f", doc)
	if r.code != cli.ExitWarnings || !strings.Contains(r.stderr, "published but not written") {
		t.Fatalf("apply: %d\n%s", r.code, r.stderr)
	}
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{},
		{"nope"},
		{"plan"},
		{"apply", "-f", "x", "--secrets-out", "a", "--secrets-fd", "3"},
		{"apply", "-f", "x", "--outputs", "yaml"},
		{"apply", "-f", "x", "--secrets-fd", "1"},
	} {
		if r := run(args...); r.code != 2 {
			t.Errorf("%v: exit %d, want 2", args, r.code)
		}
	}
	if r := run("version"); r.code != 0 || strings.TrimSpace(r.stdout) != "test" {
		t.Errorf("version: %d %q", r.code, r.stdout)
	}
}

func TestAnInterruptedApplySaysWhatWasAppliedAndWhatWasNot(t *testing.T) {
	fake, _, doc := setup(t)
	fake.FailNext("POST /api/organization-roles", 422)
	r := run("apply", "-f", doc)
	if r.code != cli.ExitError || !strings.Contains(r.stderr, "stopped: ") || !strings.Contains(r.stderr, "failed organizationRoles ") ||
		!strings.Contains(r.stderr, "not run") {
		t.Fatalf("apply: %d\n%s", r.code, r.stderr)
	}
}

func TestATransientFailureSaysARetryMaySucceed(t *testing.T) {
	fake, _, doc := setup(t)
	fake.FailNext("POST /api/organization-roles", 503)
	if r := run("apply", "-f", doc); r.code != cli.ExitError || !strings.Contains(r.stderr, "transient: a later apply may succeed") {
		t.Fatalf("apply: %d\n%s", r.code, r.stderr)
	}
}

func TestAnUntestedLogtoIsRefusedUnlessAllowed(t *testing.T) {
	fake, _, doc := setup(t)
	raw, _ := logtospecs.Spec(logtospecs.Versions()[0])
	fake.ServeSpec(bytes.Replace(raw, []byte(`"paths":{`), []byte(`"paths":{"/api/new-in-a-later-logto":{},`), 1))
	r := run("plan", "-f", doc)
	if r.code != cli.ExitError || !strings.Contains(r.stderr, "none of the supported versions") || !strings.Contains(r.stderr, "--allow-untested-logto") {
		t.Fatalf("plan: %d\n%s", r.code, r.stderr)
	}
	r = run("plan", "-f", doc, "--allow-untested-logto")
	if r.code != cli.ExitOK || !strings.Contains(r.stderr, "outside the support window") {
		t.Fatalf("plan --allow-untested-logto: %d\n%s", r.code, r.stderr)
	}
}
