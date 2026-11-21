package openapi_test

import (
	"strings"
	"testing"

	logtospecs "github.com/Aliud-Corp/logto-operator/api/logto"
	"github.com/Aliud-Corp/logto-operator/internal/logto/openapi"
)

func load(t *testing.T, version string) *openapi.Doc {
	t.Helper()
	raw, err := logtospecs.Spec(version)
	if err != nil {
		t.Fatal(err)
	}
	d, err := openapi.Load(raw)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestEveryVendoredSpecLoads(t *testing.T) {
	for _, v := range logtospecs.Versions() {
		if d := load(t, v); !d.Has("UpdateSignInExp") {
			t.Errorf("%s: no UpdateSignInExp", v)
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
	ok := map[string]any{"color": map[string]any{"primaryColor": "#5B4FE0", "isDarkModeEnabled": false, "darkPrimaryColor": "#8B5CF6"}}
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
