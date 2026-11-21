package users

import (
	"testing"

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
	for _, b := range []createUser{{PrimaryEmail: "admin@example.test", Name: "Admin"}, {PrimaryEmail: "admin@example.test"}} {
		p, err := contract.Request("CreateUser", b)
		requireValid(t, "CreateUser", p, err)
	}
	p, err := contract.Request("UpdateUser", updateUser{Name: "Admin"})
	requireValid(t, "UpdateUser", p, err)
	p, err = contract.Request("AssignUserRoles", assignRoles{RoleIDs: []string{"r"}})
	requireValid(t, "AssignUserRoles", p, err)
}

func TestTheDecodedFieldsExistInEveryVersion(t *testing.T) {
	p, err := contract.ResponseFields("ListUsers", 200, []map[string]any{{"id": "u", "primaryEmail": "a@example.test", "name": "A"}})
	requireValid(t, "ListUsers response fields", p, err)
	p, err = contract.ResponseFields("ListUserRoles", 200, []map[string]any{{"id": "r", "name": "n", "type": "User"}})
	requireValid(t, "ListUserRoles response fields", p, err)
}
