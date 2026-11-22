package roles_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindtest"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/resources"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/roles"
	"github.com/Aliud-Corp/logto-operator/internal/logto/client"
	"github.com/Aliud-Corp/logto-operator/internal/logto/logtofake"
	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

const api = `  resources:
    app:
      name: Example App API
      indicator: https://app.example.test/api
      scopes:
        "event:read": {}
        "event:write": {}
`

const declared = api + `  roles:
    reader:
      name: Event reader
      description: Reads events
      type: User
      isDefault: true
      scopes: [{resource: app, scope: "event:read"}]
    sync:
      name: Event sync
      description: Syncs events
      type: MachineToMachine
      scopes: [{resource: app, scope: "event:read"}, {resource: app, scope: "event:write"}]
`

func apply(t *testing.T, c *client.Client, doc string) {
	t.Helper()
	kindtest.Apply(t, c, kindtest.Spec(t, doc), resources.Kind{}, roles.Kind{})
}

func TestAnEmptyLogtoGetsTheRolesAndAReplayWritesNothing(t *testing.T) {
	fake, c := kindtest.Fake(t)
	apply(t, c, declared)

	got := fake.Roles()
	reader, sync := got["Event reader"], got["Event sync"]
	if reader.Type != "User" || !reader.IsDefault || strings.Join(reader.Scopes, ",") != "https://app.example.test/api event:read" {
		t.Fatalf("reader = %+v", reader)
	}
	if sync.Type != "MachineToMachine" || sync.IsDefault || len(sync.Scopes) != 2 {
		t.Fatalf("sync = %+v", sync)
	}
	writes := fake.Writes()
	kindtest.Unchanged(t, kindtest.Apply(t, c, kindtest.Spec(t, declared), resources.Kind{}, roles.Kind{}))
	if fake.Writes() != writes {
		t.Fatalf("the replay wrote %d times", fake.Writes()-writes)
	}
}

func createRole(t *testing.T, c *client.Client, body string) {
	t.Helper()
	req, err := mgmt.NewCreateRoleRequestWithBody(c.Endpoint(), "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Do(context.Background(), req, nil); err != nil {
		t.Fatal(err)
	}
}

// An existing role is adopted by name; declared scopes make its scopes exact, both ways.
func TestAnExistingRoleIsAdoptedAndHoldsExactlyTheDeclaredScopes(t *testing.T) {
	fake, c := kindtest.Fake(t)
	apply(t, c, api)
	createRole(t, c, `{"name":"Event sync","description":"Old","type":"MachineToMachine"}`)
	id := fake.Roles()["Event sync"].ID
	apply(t, c, declared)
	sync := fake.Roles()["Event sync"]
	if sync.ID != id || sync.Description != "Syncs events" || len(sync.Scopes) != 2 {
		t.Fatalf("not adopted: %+v", sync)
	}
	fewer := strings.Replace(declared, `scopes: [{resource: app, scope: "event:read"}, {resource: app, scope: "event:write"}]`, `scopes: [{resource: app, scope: "event:write"}]`, 1)
	apply(t, c, fewer)
	if got := fake.Roles()["Event sync"].Scopes; strings.Join(got, ",") != "https://app.example.test/api event:write" {
		t.Fatalf("scopes = %v, want only event:write", got)
	}
}

// Scopes left out of a declaration are not managed: what Logto holds stays.
func TestUndeclaredScopesAreLeftAsTheyAre(t *testing.T) {
	fake, c := kindtest.Fake(t)
	apply(t, c, declared)
	unmanaged := strings.Replace(declared, `      scopes: [{resource: app, scope: "event:read"}, {resource: app, scope: "event:write"}]`+"\n", "", 1)
	kindtest.Unchanged(t, kindtest.Apply(t, c, kindtest.Spec(t, unmanaged), resources.Kind{}, roles.Kind{}))
	if got := fake.Roles()["Event sync"].Scopes; len(got) != 2 {
		t.Fatalf("scopes = %v", got)
	}
}

func TestARoleTypeChangeIsRefused(t *testing.T) {
	_, c := kindtest.Fake(t)
	apply(t, c, declared)
	changed := strings.Replace(declared, "type: MachineToMachine", "type: User", 1)
	_, err := kindtest.Plan(c, kindtest.Spec(t, changed), resources.Kind{}, roles.Kind{})
	if err == nil || !strings.Contains(err.Error(), "spec.roles.sync: the role exists with type MachineToMachine") {
		t.Fatalf("err = %v", err)
	}
}

// Prune removes undeclared roles, never Logto's Management API role nor a kept one.
func TestPruneSparesTheManagementRoleAndTheKeptOnes(t *testing.T) {
	fake, c := kindtest.Fake(t)
	fake.SeedRole("Stale", "User")
	fake.SeedRole("Kept", "User")
	apply(t, c, declared+"  prune: {roles: true, keep: [Kept]}\n")
	var names []string
	for name := range fake.Roles() {
		names = append(names, name)
	}
	got := strings.Join(sorted(names), ",")
	if want := "Event reader,Event sync,Kept," + logtofake.ManagementRole; got != want {
		t.Fatalf("roles = %s, want %s", got, want)
	}
}

func sorted(l []string) []string {
	for i := range l {
		for j := i + 1; j < len(l); j++ {
			if l[j] < l[i] {
				l[i], l[j] = l[j], l[i]
			}
		}
	}
	return l
}
