package applications

import (
	"testing"

	"github.com/Aliud-Corp/logto-operator/api/v1alpha1"
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

var yes = true

var apps = []v1alpha1.Application{
	{
		Name: "Example App", Type: "SPA", Description: "The app",
		OIDCClientMetadata:   &v1alpha1.OIDCClientMetadata{RedirectURIs: []string{"https://app.example.test/callback"}, PostLogoutRedirectURIs: []string{"https://app.example.test"}},
		CustomClientMetadata: &v1alpha1.CustomClientMetadata{CORSAllowedOrigins: []string{"https://app.example.test"}, AlwaysIssueRefreshToken: &yes},
	},
	{Name: "Example Backend", Type: "MachineToMachine"},
	{Name: "Example Connector", Type: "Traditional", IsThirdParty: &yes},
}

func TestTheWireBodiesAreValidForTheWholeWindow(t *testing.T) {
	for _, d := range apps {
		p, err := contract.Request("CreateApplication", newCreate(d))
		requireValid(t, "CreateApplication "+d.Name, p, err)
		p, err = contract.Request("UpdateApplication", newUpdate(d, wireApp{}))
		requireValid(t, "UpdateApplication "+d.Name, p, err)
	}
	p, err := contract.Request("CreateApplicationUserConsentScope", addConsent{ResourceScopes: []string{"s1"}, UserScopes: []string{"profile", "email"}})
	requireValid(t, "CreateApplicationUserConsentScope", p, err)
	p, err = contract.Request("CreateApplicationSecret", createSecret{Name: SecretName})
	requireValid(t, "CreateApplicationSecret", p, err)
	p, err = contract.Request("AssignApplicationRoles", assignRoles{RoleIDs: []string{"r1"}})
	requireValid(t, "AssignApplicationRoles", p, err)
}

func TestTheDecodedFieldsExistInEveryVersion(t *testing.T) {
	for _, c := range []struct {
		op   string
		body any
	}{
		{"ListApplications", []map[string]any{{"id": "a", "name": "n", "type": "SPA", "description": nil, "isThirdParty": false, "oidcClientMetadata": map[string]any{}, "customClientMetadata": map[string]any{}}}},
		{"ListApplicationUserConsentScopes", map[string]any{
			"organizationScopes": []any{}, "organizationResourceScopes": []any{}, "userScopes": []string{"profile"},
			"resourceScopes": []map[string]any{{
				"resource": map[string]any{"id": "r", "name": "n", "indicator": "https://x.test/api"},
				"scopes":   []map[string]any{{"id": "s", "name": "read", "description": nil}},
			}},
		}},
		{"ListApplicationSecrets", []map[string]any{{"name": "Default secret", "value": "v", "expiresAt": nil}}},
		{"ListRoles", []map[string]any{{"id": "r", "name": "n", "type": "MachineToMachine"}}},
		{"ListApplicationRoles", []map[string]any{{"id": "r", "name": "n", "type": "MachineToMachine"}}},
	} {
		p, err := contract.ResponseFields(c.op, 200, c.body)
		requireValid(t, c.op+" response fields", p, err)
	}
}
