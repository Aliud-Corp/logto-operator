package kindkit_test

import (
	"strings"
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindkit"
	"github.com/Aliud-Corp/logto-operator/internal/engine/plan"
)

type declared struct{ Name string }

type held struct{ ID, Name string }

// collection is a Collection over test types: a create, update or delete op names what it was planned from.
func collection(decl map[string]declared, prune bool, keep ...string) kindkit.Collection[declared, held] {
	return kindkit.Collection[declared, held]{
		Kind:      "things",
		Where:     "spec.things",
		Declared:  decl,
		Natural:   func(_ string, d declared) string { return d.Name },
		Held:      func(h held) string { return h.Name },
		Prune:     prune,
		Keep:      keep,
		Protected: func(h held) bool { return h.Name == "builtin" },
		Create: func(key string, d declared) ([]plan.Op, error) {
			return []plan.Op{{Kind: "things", Key: key, Name: d.Name, Action: plan.Create}}, nil
		},
		Update: func(key string, d declared, h held) ([]plan.Op, error) {
			return []plan.Op{{Kind: "things", Key: key, Name: h.ID, Action: plan.Noop}}, nil
		},
		Delete: func(h held) plan.Op { return plan.Op{Kind: "things", Key: h.Name, Name: h.ID, Action: plan.Delete} },
	}
}

func render(ops []plan.Op) string {
	var out []string
	for _, op := range ops {
		out = append(out, [...]string{"noop", "create", "update", "delete"}[op.Action]+" "+op.Key+"="+op.Name)
	}
	return strings.Join(out, ", ")
}

func TestACollectionCreatesTheMissingAndUpdatesTheHeldInKeyOrder(t *testing.T) {
	c := collection(map[string]declared{"b": {Name: "beta"}, "a": {Name: "alpha"}}, false)
	ops, err := c.Plan([]held{{ID: "id-b", Name: "beta"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := render(ops), "create a=alpha, noop b=id-b"; got != want {
		t.Fatalf("ops = %s, want %s", got, want)
	}
}

func TestACollectionRefusesADeclaredObjectThatMatchesSeveral(t *testing.T) {
	c := collection(map[string]declared{"a": {Name: "alpha"}}, true)
	_, err := c.Plan([]held{{ID: "1", Name: "alpha"}, {ID: "2", Name: "alpha"}})
	if err == nil || !strings.Contains(err.Error(), `spec.things.a: 2 Logto objects match "alpha"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestACollectionRefusesTwoDeclarationsOfOneObject(t *testing.T) {
	c := collection(map[string]declared{"a": {Name: "alpha"}, "b": {Name: "alpha"}}, false)
	_, err := c.Plan(nil)
	if err == nil || !strings.Contains(err.Error(), `spec.things.b: declares "alpha" like spec.things.a`) {
		t.Fatalf("err = %v", err)
	}
}

func TestACollectionPrunesOnlyWhenAskedAndNeverTheKeptOrTheProtected(t *testing.T) {
	observed := []held{{ID: "1", Name: "alpha"}, {ID: "2", Name: "stale"}, {ID: "3", Name: "kept"}, {ID: "4", Name: "builtin"}, {ID: "5", Name: "stale"}}
	decl := map[string]declared{"a": {Name: "alpha"}}

	ops, err := collection(decl, false, "kept").Plan(observed)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := render(ops), "noop a=1"; got != want {
		t.Fatalf("without prune: ops = %s, want %s", got, want)
	}

	ops, err = collection(decl, true, "kept").Plan(observed)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := render(ops), "noop a=1, delete stale=2, delete stale=5"; got != want {
		t.Fatalf("with prune: ops = %s, want %s", got, want)
	}
}

func TestACollectionKeepsByAnyOfAnObjectsKeys(t *testing.T) {
	c := collection(nil, true, "id-2")
	c.KeepKeys = func(h held) []string { return []string{h.Name, h.ID} }
	ops, err := c.Plan([]held{{ID: "id-1", Name: "one"}, {ID: "id-2", Name: "two"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := render(ops), "delete one=id-1"; got != want {
		t.Fatalf("ops = %s, want %s", got, want)
	}
}
