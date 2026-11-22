//go:build integration

package users_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindtest"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/resources"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/roles"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/users"
	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

// On a real Logto: the admin is created, a replay plans nothing, and the same address written in another case is
// the same user.
func TestOnARealLogto(t *testing.T) {
	c := kindtest.Real(t)
	u := kindtest.Unique(t)
	role := "it_admin_" + u
	req, err := mgmt.NewCreateRoleRequestWithBody(c.Endpoint(), "application/json",
		strings.NewReader(`{"name":"`+role+`","description":"integration test role","type":"User"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Do(context.Background(), req, nil); err != nil {
		t.Fatal(err)
	}
	doc := strings.NewReplacer("admin@example.test", "admin-"+u+"@example.test", "platform_admin", role).Replace(declared)
	kindtest.Apply(t, c, kindtest.Spec(t, doc), resources.Kind{}, roles.Kind{}, users.Kind{})
	p, err := kindtest.Plan(c, kindtest.Spec(t, doc), resources.Kind{}, roles.Kind{}, users.Kind{})
	if err != nil {
		t.Fatal(err)
	}
	kindtest.Unchanged(t, p)

	upper := strings.Replace(doc, "admin-"+u+"@example.test", "Admin-"+u+"@Example.test", 1)
	p, err = kindtest.Plan(c, kindtest.Spec(t, upper), resources.Kind{}, roles.Kind{}, users.Kind{})
	if err != nil {
		t.Fatal(err)
	}
	kindtest.Unchanged(t, p)
}
