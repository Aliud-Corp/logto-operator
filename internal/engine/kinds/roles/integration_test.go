//go:build integration

package roles_test

import (
	"strings"
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindtest"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/resources"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/roles"
	"github.com/Aliud-Corp/logto-operator/internal/engine/plan"
	"github.com/Aliud-Corp/logto-operator/internal/logto/logtofake"
)

// On a real Logto: created with its scopes, a replay plans nothing, a scope dropped from the document is removed,
// prune removes an undeclared role and spares Logto's Management API role.
func TestOnARealLogto(t *testing.T) {
	c := kindtest.Real(t)
	u := kindtest.Unique(t)
	doc := strings.ReplaceAll(`  resources:
    app:
      name: IT Role API UNIQ
      indicator: https://it-role-UNIQ.example.test/api
      scopes:
        "event:read": {}
        "event:write": {}
  roles:
    sync:
      name: it-sync-UNIQ
      description: Syncs events
      type: MachineToMachine
      scopes: [{resource: app, scope: "event:read"}, {resource: app, scope: "event:write"}]
    reader:
      name: it-reader-UNIQ
      description: Reads events
      type: User
      scopes: [{resource: app, scope: "event:read"}]
`, "UNIQ", u)
	kindtest.Apply(t, c, kindtest.Spec(t, doc), resources.Kind{}, roles.Kind{})
	p, err := kindtest.Plan(c, kindtest.Spec(t, doc), resources.Kind{}, roles.Kind{})
	if err != nil {
		t.Fatal(err)
	}
	kindtest.Unchanged(t, p)

	fewer := strings.Replace(doc, `scopes: [{resource: app, scope: "event:read"}, {resource: app, scope: "event:write"}]`, `scopes: [{resource: app, scope: "event:write"}]`, 1)
	kindtest.Apply(t, c, kindtest.Spec(t, fewer), resources.Kind{}, roles.Kind{})
	p, err = kindtest.Plan(c, kindtest.Spec(t, fewer), resources.Kind{}, roles.Kind{})
	if err != nil {
		t.Fatal(err)
	}
	kindtest.Unchanged(t, p)

	// Drop the reader role and prune. Only this test's objects are candidates (KeepAllBut), except Logto's
	// Management API role, taken out of keep: the kind itself must spare it.
	i := strings.Index(fewer, "    reader:\n")
	spec := kindtest.KeepAllBut(t, c, kindtest.Spec(t, fewer[:i]+"  prune: {roles: true}\n"), u)
	var keep []string
	for _, k := range spec.Prune.Keep {
		if k != logtofake.ManagementRole {
			keep = append(keep, k)
		}
	}
	spec.Prune.Keep = keep
	kindtest.Apply(t, c, spec, resources.Kind{}, roles.Kind{})
	p, err = kindtest.Plan(c, kindtest.Spec(t, fewer), resources.Kind{}, roles.Kind{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Count(plan.Create) != 1 {
		t.Fatalf("after prune, the reader role must be gone and planned again:\n%v", p)
	}
	app := strings.Replace(fewer[:i], "  roles:\n", "  roles:\n    mgmt: {name: "+logtofake.ManagementRole+", description: kept, type: MachineToMachine}\n", 1)
	p, err = kindtest.Plan(c, kindtest.Spec(t, app), resources.Kind{}, roles.Kind{})
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range p.Ops {
		if op.Key == "mgmt" && op.Action == plan.Create {
			t.Fatal("prune deleted Logto's Management API role")
		}
	}
}
