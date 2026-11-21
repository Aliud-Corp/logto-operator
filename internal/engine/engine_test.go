package engine_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"sort"
	"strings"
	"syscall"
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/engine"
	"github.com/Aliud-Corp/logto-operator/internal/engine/plan"
	"github.com/Aliud-Corp/logto-operator/internal/logto/capability"
	"github.com/Aliud-Corp/logto-operator/internal/logto/client"
)

// remote is an in-memory "Logto" shared by the fake kinds: objects by kind and name, and a write counter.
type remote struct {
	objects map[string]map[string]string // kind -> name -> value
	writes  int
	drop    bool     // when set, writes are acknowledged but not kept
	log     []string // "kind action key", in write order
}

func (r *remote) put(kind, name, value string) {
	r.writes++
	r.log = append(r.log, kind+" put "+name)
	if r.drop {
		return
	}
	if r.objects[kind] == nil {
		r.objects[kind] = map[string]string{}
	}
	r.objects[kind][name] = value
}

func (r *remote) del(kind, name string) {
	r.writes++
	r.log = append(r.log, kind+" delete "+name)
	delete(r.objects[kind], name)
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
	prune   bool             // undeclared objects of the kind are deleted
	fail    map[string]error // Apply of these keys fails
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
	for _, key := range sortedKeys(k.desired) {
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
	if k.prune {
		for _, name := range sortedKeys(k.r.objects[k.name]) {
			if _, ok := k.desired[name]; !ok {
				ops = append(ops, plan.Op{Kind: k.name, Key: name, Action: plan.Delete})
			}
		}
	}
	return ops, nil
}

func (k *fakeKind) Apply(_ context.Context, env *engine.Env, op plan.Op) error {
	if err := k.fail[op.Key]; err != nil {
		return err
	}
	if op.Action == plan.Delete {
		k.r.del(k.name, op.Key)
		return nil
	}
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
	if _, err := e.Apply(context.Background(), &engine.Env{SkipCapability: true}); err != nil {
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
	p, err := e.Plan(context.Background(), &engine.Env{SkipCapability: true})
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
	if _, err := e.Apply(context.Background(), &engine.Env{SkipCapability: true}); err != nil {
		t.Fatal(err)
	}
	writes := r.writes
	p, err := e.Apply(context.Background(), &engine.Env{SkipCapability: true})
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
	_, err := e.Apply(context.Background(), &engine.Env{SkipCapability: true})
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
	_, err = e.Apply(context.Background(), &engine.Env{Capability: spec, AllowUntested: true})
	if err == nil || !strings.Contains(err.Error(), "this Logto has no NoSuchOperation operation") {
		t.Fatalf("err = %v", err)
	}
	if r.writes != 0 {
		t.Fatalf("%d writes before the refusal", r.writes)
	}
}

// A delete runs once every kind has created and updated what it declares, kinds in reverse order: a resource is
// deleted only after the roles that pointed at it were updated, a role before the resource it depended on.
func TestPruneRunsLastInReverseKindOrder(t *testing.T) {
	r, _, resources, roles := setup()
	r.objects["resources"] = map[string]string{"stray": "x"}
	r.objects["roles"] = map[string]string{"stray": "x"}
	resources.prune, roles.prune = true, true
	e, _ := engine.New(resources, roles)
	if _, err := e.Apply(context.Background(), &engine.Env{SkipCapability: true}); err != nil {
		t.Fatal(err)
	}
	want := "resources put app,roles put owner,roles delete stray,resources delete stray"
	if got := strings.Join(r.log, ","); got != want {
		t.Fatalf("writes = %s\nwant     %s", got, want)
	}
}

// A failed apply says, op by op, what was applied, what failed and what was never run, and whether the failure is
// worth a retry.
func TestAFailedApplyReportsEachOpAndWhetherToRetry(t *testing.T) {
	for _, tc := range []struct {
		err       error
		transient bool
	}{
		{&client.APIError{Method: "POST", Path: "/api/roles", Status: 503}, true},
		{&client.APIError{Method: "POST", Path: "/api/roles", Status: 429}, true},
		{&client.APIError{Method: "POST", Path: "/api/roles", Status: 422}, false},
		{errors.New("roles: no such resource"), false},
	} {
		_, _, resources, roles := setup()
		roles.desired = map[string]string{"owner": "owner", "zz-member": "member"}
		roles.fail = map[string]error{"owner": tc.err}
		e, _ := engine.New(resources, roles)
		p, err := e.Apply(context.Background(), &engine.Env{SkipCapability: true})
		var opErr *engine.OpError
		if !errors.As(err, &opErr) || opErr.Kind != "roles" || opErr.Key != "owner" || !errors.Is(err, tc.err) {
			t.Fatalf("err = %v", err)
		}
		if engine.Transient(err) != tc.transient {
			t.Errorf("%v: transient = %v, want %v", tc.err, engine.Transient(err), tc.transient)
		}
		got := map[string]plan.Result{}
		for _, op := range p.Ops {
			got[op.Kind+"/"+op.Key] = op.Result
		}
		if got["resources/app"] != plan.Applied || got["roles/owner"] != plan.Failed || got["roles/zz-member"] != plan.NotRun {
			t.Fatalf("results = %v", got)
		}
	}
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestTransientTellsARetryWorthyFailureFromAFinalOne(t *testing.T) {
	for err, want := range map[error]bool{
		&client.AuthError{Status: 503}: true,
		&client.AuthError{Status: 401}: false,
		&url.Error{Op: "Get", URL: "https://auth.example.test", Err: &net.OpError{Op: "dial", Err: errors.New("connection refused")}}: true,
		&url.Error{Op: "Get", URL: "https://auth.example.test", Err: x509.UnknownAuthorityError{}}:                                    false,
		&url.Error{Op: "Get", URL: "https://auth.example.test", Err: &tls.CertificateVerificationError{Err: errors.New("bad")}}:       false,
		context.Canceled: false,
		&url.Error{Op: "Post", URL: "https://auth.example.test", Err: io.EOF}:                                                                         true,
		&url.Error{Op: "Get", URL: "https://auth.example.test", Err: &net.DNSError{Err: "no such host", Name: "auth.example.test", IsNotFound: true}}: false,
		&url.Error{Op: "Get", URL: "https://auth.example.test", Err: errors.New("http: server gave HTTP response to HTTPS client")}:                   false,
		&url.Error{Op: "Get", URL: "https://auth.example.test", Err: &net.OpError{Op: "read", Err: syscall.ECONNRESET}}:                               true,
		&url.Error{Op: "Get", URL: "https://auth.example.test", Err: timeoutErr{}}:                                                                    true,
	} {
		if got := engine.Transient(fmt.Errorf("wrapped: %w", err)); got != want {
			t.Errorf("%T %v: transient = %v, want %v", err, err, got, want)
		}
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// A Logto whose spec is none of the supported versions is refused before any call, unless the run allows it: the
// engine decides, for the CLI and the operator alike.
func TestAnUntestedLogtoIsRefusedByTheEngineUnlessAllowed(t *testing.T) {
	r, _, resources, _ := setup()
	untested, err := capability.Parse([]byte(`{"openapi":"3.0.1","info":{"title":"t","version":"1"},"paths":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	e, _ := engine.New(resources)
	_, err = e.Apply(context.Background(), &engine.Env{Capability: untested})
	if err == nil || !strings.Contains(err.Error(), "none of the supported versions") || r.writes != 0 {
		t.Fatalf("err = %v, %d writes", err, r.writes)
	}
	if _, err := e.Apply(context.Background(), &engine.Env{Capability: untested, AllowUntested: true}); err != nil {
		t.Fatalf("allowed: %v", err)
	}
}
