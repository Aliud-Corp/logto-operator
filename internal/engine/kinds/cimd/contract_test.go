package cimd

import (
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/consentscopes"
	"github.com/Aliud-Corp/logto-operator/internal/logto/contract"
)

func requireValid(t *testing.T, what string, p contract.Problems, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	for v, problems := range p {
		t.Errorf("%s is invalid for Logto %s: %v", what, v, problems)
	}
}

func TestTheWireBodiesAreValidForTheWholeWindow(t *testing.T) {
	yes := true
	for name, body := range map[string]any{
		"every key": wireConfig{Enabled: true, AddConsentPromptForOfflineAccess: &yes},
		"enabled":   wireConfig{Enabled: false},
	} {
		p, err := contract.Request("UpdateCimdConfig", body)
		requireValid(t, "UpdateCimdConfig ("+name+")", p, err)
	}
	// The PATCH the kind sends carries the changed keys only.
	p, err := contract.Request("UpdateCimdConfig", map[string]any{"addConsentPromptForOfflineAccess": true})
	requireValid(t, "UpdateCimdConfig (one key)", p, err)
	p, err = contract.Request("AssignCimdUserConsentScopes", consentscopes.Add{ResourceScopes: []string{"s1"}, UserScopes: []string{"profile", "email"}})
	requireValid(t, "AssignCimdUserConsentScopes", p, err)
}

func TestTheDecodedFieldsExistInEveryVersion(t *testing.T) {
	for _, c := range []struct {
		op   string
		body any
	}{
		{"GetCimdConfig", map[string]any{"enabled": true, "addConsentPromptForOfflineAccess": false}},
		{"ListCimdUserConsentScopes", map[string]any{
			"organizationScopes": []any{}, "organizationResourceScopes": []any{}, "userScopes": []string{"profile"},
			"resourceScopes": []map[string]any{{
				"resource": map[string]any{"id": "r", "name": "n", "indicator": "https://x.test/api"},
				"scopes":   []map[string]any{{"id": "s", "name": "read", "description": nil}},
			}},
		}},
	} {
		p, err := contract.ResponseFields(c.op, 200, c.body)
		requireValid(t, c.op+" response fields", p, err)
	}
}
