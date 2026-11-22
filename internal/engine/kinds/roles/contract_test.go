package roles

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

func TestTheWireBodiesAreValidForTheWholeWindow(t *testing.T) {
	yes := true
	for _, d := range []v1alpha1.Role{
		{Name: "Reader", Description: "Reads", Type: "User", IsDefault: &yes},
		{Name: "Sync", Description: "Syncs", Type: "MachineToMachine"},
	} {
		p, err := contract.Request("CreateRole", newCreate(d, []string{"s1"}))
		requireValid(t, "CreateRole", p, err)
		p, err = contract.Request("CreateRole", newCreate(d, nil))
		requireValid(t, "CreateRole without scopes", p, err)
	}
	desc := "Reads"
	p, err := contract.Request("UpdateRole", updateRole{Description: &desc, IsDefault: &yes})
	requireValid(t, "UpdateRole", p, err)
	p, err = contract.Request("CreateRoleScope", addScopes{ScopeIDs: []string{"s1"}})
	requireValid(t, "CreateRoleScope", p, err)
}

func TestTheDecodedFieldsExistInEveryVersion(t *testing.T) {
	p, err := contract.Response("ListRoles", 200, []map[string]any{{
		"tenantId": "default", "id": "r1", "name": "Reader", "description": "Reads", "type": "User", "isDefault": false,
		"usersCount": 0, "featuredUsers": []any{}, "applicationsCount": 0, "featuredApplications": []any{},
	}})
	requireValid(t, "ListRoles response", p, err)
	p, err = contract.Response("ListRoleScopes", 200, []map[string]any{{
		"tenantId": "default", "id": "s1", "resourceId": "res1", "name": "event:read", "description": nil, "createdAt": 1,
		"resource": map[string]any{"tenantId": "default", "id": "res1", "name": "App", "indicator": "https://app.example.test/api", "isDefault": false, "accessTokenTtl": 3600},
	}})
	requireValid(t, "ListRoleScopes response", p, err)
}
