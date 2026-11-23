package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/config"
)

const minimal = `apiVersion: logto.aliud.fr/v1alpha1
kind: LogtoConfig
metadata: {name: t}
spec:
  endpoint: ${endpoint}
  credentials:
    clientId: {value: client}
    clientSecret: {valueFrom: {env: SECRET}}
  variables:
    endpoint: http://localhost:3001
    zone: example.test
`

func TestTheReferenceDocumentLoadsAndValidates(t *testing.T) {
	doc, err := config.Load("../../examples/reference.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := config.Validate(doc.Config); err != nil {
		t.Fatalf("the reference document is invalid:\n%v", err)
	}
	s := doc.Config.Spec
	if s.Endpoint != "http://localhost:3001" {
		t.Errorf("endpoint = %q, variables not interpolated", s.Endpoint)
	}
	if got := s.Resources["app"].Indicator; got != "https://app.example.test/api" {
		t.Errorf("indicator = %q", got)
	}
	if logo := s.SignInExperience.Branding.LogoURL.Value; !strings.HasPrefix(logo, "data:image/svg+xml;base64,") {
		t.Errorf("logo not turned into a data URI: %.40q", logo)
	}
	if css := s.SignInExperience.CustomCSS; css.FromFile != "" || !strings.Contains(css.Value, "border-radius") {
		t.Errorf("custom CSS not read from its file: %+v", css)
	}
	if n := len(s.Connectors["smtp"].SMTP.Templates.Items); n != 4 {
		t.Errorf("%d templates read from fromDir, want 4", n)
	}
	for _, item := range s.Connectors["smtp"].SMTP.Templates.Items {
		if item.Content.FromFile != "" || !strings.Contains(item.Content.Value, "{{code}}") {
			t.Errorf("template %s: body not read: %+v", item.UsageType, item.Content)
		}
	}
}

func TestAnUnknownKeyFailsWithItsPath(t *testing.T) {
	_, err := config.Parse([]byte(minimal + "  applications:\n    app: {name: A, type: SPA, colour: blue}\n"))
	if err == nil || !strings.Contains(err.Error(), "spec.applications.app.colour: unknown field") {
		t.Fatalf("err = %v", err)
	}
}

func TestAnUndeclaredVariableFailsWithItsPath(t *testing.T) {
	_, err := config.Parse([]byte(minimal + "  signInExperience: {unknownSessionRedirectUrl: \"https://${host}\"}\n"))
	if err == nil || !strings.Contains(err.Error(), "spec.signInExperience.unknownSessionRedirectUrl: ${host} is not declared") {
		t.Fatalf("err = %v", err)
	}
}

func TestADoubleDollarWritesALiteral(t *testing.T) {
	cfg, err := config.Parse([]byte(minimal + "  signInExperience: {unknownSessionRedirectUrl: \"https://$${zone}\"}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Spec.SignInExperience.UnknownSessionRedirectURL; got != "https://${zone}" {
		t.Fatalf("got %q", got)
	}
}

func TestAWrongKindIsRefused(t *testing.T) {
	_, err := config.Parse([]byte(strings.Replace(minimal, "kind: LogtoConfig", "kind: Other", 1)))
	if err == nil || !strings.Contains(err.Error(), "apiVersion/kind") {
		t.Fatalf("err = %v", err)
	}
}

func TestAMissingFileFailsWithItsPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "c.yaml")
	body := minimal + "  signInExperience: {customCss: {fromFile: nope.css}}\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load(path)
	if err == nil || !strings.Contains(err.Error(), "spec.signInExperience.customCss: fromFile") {
		t.Fatalf("err = %v", err)
	}
}

// A template read from a directory names its body relative to that directory, so two documents in different
// directories share one set of template files.
func TestATemplateBodyIsReadNextToItsTemplate(t *testing.T) {
	root := t.TempDir()
	write := func(name, body string) {
		t.Helper()
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("shared/templates/register.json",
		`{"usageType": "Register", "contentType": "text/html", "subject": "s", "content": {"fromFile": "register.html"}}`)
	write("shared/templates/register.html", "<p>{{code}}</p>\n")
	write("stag/c.yaml", minimal+`  connectors:
    smtp:
      id: smtp
      connectorId: simple-mail-transfer-protocol
      smtp:
        host: smtp.${zone}
        port: 587
        auth: {user: {value: u}, pass: {valueFrom: {env: PASS}}}
        fromEmail: no-reply@${zone}
        templates: {fromDir: ../shared/templates}
`)
	doc, err := config.Load(filepath.Join(root, "stag/c.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	items := doc.Config.Spec.Connectors["smtp"].SMTP.Templates.Items
	if len(items) != 1 || items[0].Content.Value != "<p>{{code}}</p>\n" || items[0].Content.FromFile != "" {
		t.Fatalf("items = %+v", items)
	}
}
