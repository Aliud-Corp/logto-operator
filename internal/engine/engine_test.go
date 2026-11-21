package engine_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/engine"
	"github.com/Aliud-Corp/logto-operator/internal/engine/plan"
	"github.com/Aliud-Corp/logto-operator/internal/logto/capability"
)

// remote is an in-memory "Logto" shared by the fake kinds: objects by kind and name, and a write counter.
type remote struct {
	objects map[string]map[string]string // kind -> name -> value
	writes  int
	drop    bool // when set, writes are acknowledged but not kept
}

func (r *remote) put(kind, name, value string) {
	r.writes++
	if r.drop {
		return
	}
	if r.objects[kind] == nil {
		r.objects[kind] = map[string]string{}
	}
	r.objects[kind][name] = value
}

// fakeKind declares objects (name -> value); it records the id of each object as a ref, and when ref is set it
// writes the id of the object it points at (another kind's) into its own value, proving refs flow between kinds.
type fakeKind struct {
	name    string
	deps    []string
	desired map[string]string
	ref     string // "kind/key": value written is "<desired>@<id of that ref>"
	r       *remote
	order   *[]string
	needs   []capability.Need
}

func (k *fakeKind) Name() string                                 { return k.name }
func (k *fakeKind) DependsOn() []string                          { return k.deps }
func (k *fakeKind) Needs(*engine.Env) ([]capability.Need, error) { return k.needs, nil }

func (k *fakeKind) want(env *engine.Env, key string) string {
	v := k.desired[key]
	if k.ref != "" {
		kind, rk, _ := strings.Cut(k.ref, "/")
		v += "@" + env.Refs.Value(kind, rk, "id")
	}
	return v
}

func (k *fakeKind) Plan(_ context.Context, env *engine.Env) ([]plan.Op, error) {
	*k.order = append(*k.order, k.name)
	var ops []plan.Op
	for key := range k.desired {
		have, exists := k.r.objects[k.name][key]
		want := k.want(env, key)
		switch {
		case !exists:
			ops = append(ops, plan.Op{Kind: k.name, Key: key, Action: plan.Create, Changes: []plan.Change{{Field: "value", To: want}}})
		case have != want:
			env.Refs.Set(k.name, key, "id", "id-"+key)
			ops = append(ops, plan.Op{Kind: k.name, Key: key, Action: plan.Update, Changes: []plan.Change{{Field: "value", From: have, To: want}}})
		default:
			env.Refs.Set(k.name, key, "id", "id-"+key)
			ops = append(ops, plan.Op{Kind: k.name, Key: key, Action: plan.Noop})
		}
	}
	return ops, nil
}

func (k *fakeKind) Apply(_ context.Context, env *engine.Env, op plan.Op) error {
	k.r.put(k.name, op.Key, k.want(env, op.Key))
	env.Refs.Set(k.name, op.Key, "id", "id-"+op.Key)
	return nil
}

func setup() (*remote, *[]string, *fakeKind, *fakeKind) {
	r := &remote{objects: map[string]map[string]string{}}
	order := &[]string{}
	resources := &fakeKind{name: "resources", desired: map[string]string{"app": "App API"}, r: r, order: order}
	roles := &fakeKind{name: "roles", deps: []string{"resources"}, desired: map[string]string{"owner": "owner"}, ref: "resources/app", r: r, order: order}
	return r, order, resources, roles
}

func TestKindsRunInDependencyOrderAndRefsFlow(t *testing.T) {
	r, order, resources, roles := setup()
	e, err := engine.New(roles, resources) // registered in the wrong order on purpose
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(e.Kinds(), ","); got != "resources,roles" {
		t.Fatalf("order = %s", got)
	}
	if _, err := e.Apply(context.Background(), &engine.Env{}); err != nil {
		t.Fatal(err)
	}
	if (*order)[0] != "resources" {
		t.Fatalf("roles planned before resources: %v", *order)
	}
	if got := r.objects["roles"]["owner"]; got != "owner@id-app" {
		t.Fatalf("roles/owner = %q, the ref of resources/app did not flow", got)
	}
}

func TestAPlanBeforeApplyShowsIdsAsKnownAfterApply(t *testing.T) {
	_, _, resources, roles := setup()
	e, _ := engine.New(resources, roles)
	p, err := e.Plan(context.Background(), &engine.Env{})
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range p.Ops {
		if op.Kind == "roles" && !strings.Contains(op.Changes[0].To, plan.KnownAfterApply) {
			t.Fatalf("roles change = %q", op.Changes[0].To)
		}
	}
}

func TestAReplayPlansNothingAndWritesNothing(t *testing.T) {
	r, _, resources, roles := setup()
	e, _ := engine.New(resources, roles)
	if _, err := e.Apply(context.Background(), &engine.Env{}); err != nil {
		t.Fatal(err)
	}
	writes := r.writes
	p, err := e.Apply(context.Background(), &engine.Env{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Changed() || r.writes != writes {
		t.Fatalf("replay changed something: plan changed=%v, writes %d -> %d", p.Changed(), writes, r.writes)
	}
}

func TestAWriteLogtoDidNotKeepFailsTheApply(t *testing.T) {
	r, _, resources, _ := setup()
	r.drop = true
	e, _ := engine.New(resources)
	_, err := e.Apply(context.Background(), &engine.Env{})
	if err == nil || !strings.Contains(err.Error(), "resources app: Logto did not keep what was written (value)") {
		t.Fatalf("err = %v", err)
	}
}

func TestACycleIsRefused(t *testing.T) {
	r := &remote{objects: map[string]map[string]string{}}
	a := &fakeKind{name: "a", deps: []string{"b"}, r: r, order: &[]string{}}
	b := &fakeKind{name: "b", deps: []string{"a"}, r: r, order: &[]string{}}
	if _, err := engine.New(a, b); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("err = %v", err)
	}
}

func TestAMissingDependencyIsRefused(t *testing.T) {
	r := &remote{objects: map[string]map[string]string{}}
	a := &fakeKind{name: "a", deps: []string{"ghost"}, r: r, order: &[]string{}}
	if _, err := engine.New(a); err == nil || !strings.Contains(err.Error(), "ghost") {
		t.Fatalf("err = %v", err)
	}
}

func TestACapabilityRefusalStopsBeforeAnyWrite(t *testing.T) {
	r, _, resources, _ := setup()
	resources.needs = []capability.Need{{OperationID: "NoSuchOperation", Where: "spec.resources"}}
	spec, err := capability.Parse([]byte(`{"openapi":"3.0.1","info":{"title":"t","version":"1"},"paths":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	e, _ := engine.New(resources)
	_, err = e.Apply(context.Background(), &engine.Env{Capability: spec})
	if err == nil || !strings.Contains(err.Error(), "this Logto has no NoSuchOperation operation") {
		t.Fatalf("err = %v", err)
	}
	if r.writes != 0 {
		t.Fatalf("%d writes before the refusal", r.writes)
	}
}
