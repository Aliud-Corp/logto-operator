package openapi_test

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	logtospecs "github.com/Aliud-Corp/logto-operator/api/logto"
	"github.com/Aliud-Corp/logto-operator/internal/logto/openapi"
)

// load is a vendored spec, from its normalized form (task generate), loaded once per version.
func load(t *testing.T, version string) *openapi.Doc {
	t.Helper()
	loaded.once.Do(func() { loaded.docs = map[string]*openapi.Doc{} })
	if d, ok := loaded.docs[version]; ok {
		return d
	}
	norm, err := logtospecs.Normalized(version)
	if err != nil {
		t.Fatal(err)
	}
	d, err := openapi.LoadNormalized(norm)
	if err != nil {
		t.Fatal(err)
	}
	loaded.docs[version] = d
	return d
}

var loaded struct {
	once sync.Once
	docs map[string]*openapi.Doc
}

// Every vendored spec loads as Logto serves it, and its normalized form is exactly what Normalize makes of it now
// (otherwise task generate was not run, and the tests would validate against a stale form).
func TestEveryVendoredSpecLoadsAndItsNormalizedFormIsCurrent(t *testing.T) {
	for _, v := range logtospecs.Versions() {
		raw, err := logtospecs.Spec(v)
		if err != nil {
			t.Fatal(err)
		}
		d, err := openapi.Load(raw)
		if err != nil || !d.Has("UpdateSignInExp") {
			t.Fatalf("%s: %v", v, err)
		}
		norm, err := openapi.Normalize(raw)
		if err != nil {
			t.Fatal(err)
		}
		stored, err := logtospecs.Normalized(v)
		if err != nil || !bytes.Equal(norm, stored) {
			t.Errorf("%s: api/logto/%s.normalized.json is stale: run task generate", v, v)
		}
	}
}

// Logto writes patterns as JavaScript literals (/^#[\da-f]{3}([\da-f]{3})?$/i); loaded as is, a valid colour would be
// refused.
func TestAJavaScriptPatternIsHonoured(t *testing.T) {
	s, err := load(t, "1.46.0").RequestSchema("UpdateSignInExp")
	if err != nil {
		t.Fatal(err)
	}
	ok := map[string]any{"color": map[string]any{"primaryColor": "#101418", "isDarkModeEnabled": false, "darkPrimaryColor": "#8B5CF6"}}
	if p := openapi.Validate(s, ok); len(p) > 0 {
		t.Fatalf("a valid colour is refused: %v", p)
	}
	bad := map[string]any{"color": map[string]any{"primaryColor": "blue", "isDarkModeEnabled": false, "darkPrimaryColor": "#8B5CF6"}}
	if p := openapi.Validate(s, bad); len(p) == 0 {
		t.Fatal("an invalid colour is accepted")
	}
}

func TestAKeyTheSchemaDoesNotDeclareIsReported(t *testing.T) {
	s, err := load(t, "1.46.0").RequestSchema("UpdateSignInExp")
	if err != nil {
		t.Fatal(err)
	}
	p := openapi.Validate(s, map[string]any{"colour": "x"})
	if len(p) != 1 || !strings.Contains(p[0], "colour: property unknown to this version") {
		t.Fatalf("problems = %v", p)
	}
}

// A union (Logto's profile fields are one schema per type) accepts a value only through a branch that fits it whole:
// the parts of a Fullname config are known to the Fullname branch, not to the Text one.
func TestAUnionIsCheckedBranchByBranch(t *testing.T) {
	s, err := load(t, "1.46.0").RequestSchema("CreateCustomProfileField")
	if err != nil {
		t.Fatal(err)
	}
	parts := map[string]any{"parts": []any{map[string]any{"enabled": true, "name": "givenName", "type": "Text", "label": "Given", "required": true}}}
	fullname := map[string]any{"name": "fullname", "type": "Fullname", "label": "Name", "required": true, "config": parts}
	if p := openapi.Validate(s, fullname); len(p) > 0 {
		t.Fatalf("a valid Fullname field is refused: %v", p)
	}
	text := map[string]any{"name": "company", "type": "Text", "label": "Company", "required": false, "config": parts}
	if p := openapi.Validate(s, text); len(p) == 0 {
		t.Fatal("a Text field carrying Fullname parts is accepted")
	}
}

// A redirect URI is exported by Logto as an object "Validator function"; it is a string, and must be accepted.
func TestAValidatorFunctionFieldAcceptsAString(t *testing.T) {
	s, err := load(t, "1.46.0").RequestSchema("CreateApplication")
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"name": "a", "type": "SPA", "oidcClientMetadata": map[string]any{
		"redirectUris": []any{"https://app.example.test/callback"}, "postLogoutRedirectUris": []any{},
	}}
	if p := openapi.Validate(s, body); len(p) > 0 {
		t.Fatalf("problems = %v", p)
	}
}

func TestARequestFindsItsOperationALiteralSegmentFirst(t *testing.T) {
	d := load(t, logtospecs.Versions()[0])
	for call, want := range map[string]string{
		"GET /api/applications/abc":         "GetApplication",
		"PATCH /api/sign-in-exp":            "UpdateSignInExp",
		"GET /api/applications/abc/secrets": "ListApplicationSecrets",
		"GET /api/configs/oidc/session":     "GetOidcSessionConfig",
		"GET /api/configs/oidc/cookie":      "GetOidcKeys",
	} {
		method, path, _ := strings.Cut(call, " ")
		if got, ok := d.Find(method, path); !ok || got != want {
			t.Errorf("%s = %q, want %q", call, got, want)
		}
	}
	if _, ok := d.Find("GET", "/api/no-such-route"); ok {
		t.Error("an unknown route was found")
	}
}

func TestFieldsReportsAWrongTypeButNotAMissingField(t *testing.T) {
	d := load(t, logtospecs.Versions()[0])
	s, err := d.ResponseSchema("GetApplication", 200)
	if err != nil {
		t.Fatal(err)
	}
	if p := openapi.Fields(s, map[string]any{"name": "a"}); len(p) != 0 {
		t.Fatalf("a partial answer is refused: %v", p)
	}
	if p := openapi.Fields(s, map[string]any{"name": 12}); len(p) == 0 || !strings.Contains(strings.Join(p, " "), "name") {
		t.Fatalf("a wrong type passes: %v", p)
	}
}

func TestAShortLiteralSegmentWinsOverAParameter(t *testing.T) {
	d, err := openapi.Load([]byte(`{"openapi":"3.0.1","info":{"title":"t","version":"1"},"paths":{` +
		`"/api/users/{userId}":{"get":{"operationId":"GetUser","responses":{"200":{"description":"ok"}}}},` +
		`"/api/users/me":{"get":{"operationId":"GetMe","responses":{"200":{"description":"ok"}}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{"/api/users/me": "GetMe", "/api/users/u1": "GetUser"} {
		if got, _ := d.Find("GET", path); got != want {
			t.Errorf("%s = %q, want %q", path, got, want)
		}
	}
}
