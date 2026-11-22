package users_test

import (
	"strings"
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindtest"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/resources"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/roles"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/users"
)

const declared = `  users:
    admin:
      primaryEmail: admin@example.test
      name: Example Admin
      roles:
        - {name: platform_admin}
`

func TestAnAdminIsCreatedWithoutAPasswordAndAReplayWritesNothing(t *testing.T) {
	fake, c := kindtest.Fake(t)
	fake.SeedRole("platform_admin", "User")
	spec := kindtest.Spec(t, declared)
	kindtest.Apply(t, c, spec, resources.Kind{}, roles.Kind{}, users.Kind{})
	u, ok := fake.Users()["admin@example.test"]
	if !ok || u.Name != "Example Admin" || u.HasPassword || strings.Join(u.Roles, ",") != "platform_admin" {
		t.Fatalf("user = %+v (found %v)", u, ok)
	}
	writes := fake.Writes()
	kindtest.Unchanged(t, kindtest.Apply(t, c, spec, resources.Kind{}, roles.Kind{}, users.Kind{}))
	if fake.Writes() != writes {
		t.Fatalf("the replay wrote %d times", fake.Writes()-writes)
	}
}

func TestAnExistingUserIsMatchedByEmailCaseInsensitively(t *testing.T) {
	fake, c := kindtest.Fake(t)
	fake.SeedRole("platform_admin", "User")
	kindtest.Apply(t, c, kindtest.Spec(t, declared), resources.Kind{}, roles.Kind{}, users.Kind{})
	upper := strings.Replace(declared, "admin@example.test", "Admin@Example.test", 1)
	p, err := kindtest.Plan(c, kindtest.Spec(t, upper), resources.Kind{}, roles.Kind{}, users.Kind{})
	if err != nil {
		t.Fatal(err)
	}
	kindtest.Unchanged(t, p)
	if n := len(fake.Users()); n != 1 {
		t.Fatalf("%d users", n)
	}
}

func TestAMissingRoleIsRefused(t *testing.T) {
	_, c := kindtest.Fake(t)
	_, err := kindtest.Plan(c, kindtest.Spec(t, declared), resources.Kind{}, roles.Kind{}, users.Kind{})
	if err == nil || !strings.Contains(err.Error(), `Logto has no User role named "platform_admin"`) {
		t.Fatalf("err = %v", err)
	}
}
