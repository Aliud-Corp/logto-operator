package organizations

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
	for _, s := range []v1alpha1.Scope{{Description: "Read members"}, {}} {
		p, err := contract.Request("CreateOrganizationScope", newScope("members:read", s))
		requireValid(t, "CreateOrganizationScope", p, err)
	}
	desc := "Read"
	p, err := contract.Request("UpdateOrganizationScope", updateScope{Description: &desc})
	requireValid(t, "UpdateOrganizationScope", p, err)
	for _, d := range []v1alpha1.OrganizationRole{{Name: "owner", Description: "Owns"}, {Name: "member"}} {
		p, err = contract.Request("CreateOrganizationRole", newRole(d, []string{"s1"}, []string{}))
		requireValid(t, "CreateOrganizationRole", p, err)
	}
	p, err = contract.Request("UpdateOrganizationRole", updateRole{Description: "Owns"})
	requireValid(t, "UpdateOrganizationRole", p, err)
	p, err = contract.Request("ReplaceOrganizationRoleScopes", replaceScopes{OrganizationScopeIDs: []string{"s1"}})
	requireValid(t, "ReplaceOrganizationRoleScopes", p, err)
	p, err = contract.Request("ReplaceOrganizationRoleResourceScopes", replaceResourceScopes{ScopeIDs: []string{}})
	requireValid(t, "ReplaceOrganizationRoleResourceScopes", p, err)
}

func TestTheDecodedFieldsExistInEveryVersion(t *testing.T) {
	p, err := contract.Response("ListOrganizationScopes", 200, []map[string]any{{"tenantId": "default", "id": "s1", "name": "members:read", "description": "Read"}})
	requireValid(t, "ListOrganizationScopes response", p, err)
	p, err = contract.Response("ListOrganizationRoles", 200, []map[string]any{{
		"tenantId": "default", "id": "r1", "name": "owner", "description": "Owns", "type": "User",
		"scopes":         []map[string]any{{"id": "s1", "name": "members:read"}},
		"resourceScopes": []map[string]any{{"id": "rs1", "name": "event:read", "resource": map[string]any{"id": "res1", "name": "App API"}}},
	}})
	requireValid(t, "ListOrganizationRoles response", p, err)
}
