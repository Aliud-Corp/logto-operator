// Package kindtest runs kinds in tests: against the in-process fake (Fake), or against the throwaway real Logto of
// the integration harness (Real, build tag `integration`).
package kindtest

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	logtospecs "github.com/Aliud-Corp/logto-operator/api/logto"
	"github.com/Aliud-Corp/logto-operator/api/v1alpha1"
	"github.com/Aliud-Corp/logto-operator/internal/config"
	"github.com/Aliud-Corp/logto-operator/internal/engine"
	"github.com/Aliud-Corp/logto-operator/internal/engine/plan"
	"github.com/Aliud-Corp/logto-operator/internal/logto/capability"
	"github.com/Aliud-Corp/logto-operator/internal/logto/client"
	"github.com/Aliud-Corp/logto-operator/internal/logto/contract"
	"github.com/Aliud-Corp/logto-operator/internal/logto/logtofake"
	"github.com/Aliud-Corp/logto-operator/internal/secrets"
)

// Header is the start of a document whose spec the tests complete.
const Header = `apiVersion: logto.aliud.fr/v1alpha1
kind: LogtoConfig
metadata: {name: test}
spec:
  endpoint: http://localhost:3001
  credentials:
    clientId: {value: test}
    clientSecret: {valueFrom: {env: LOGTO_TEST_SECRET}}
`

// Spec parses and validates Header followed by the given spec body (indented under spec:).
func Spec(t *testing.T, body string) *v1alpha1.LogtoConfigSpec {
	t.Helper()
	cfg, err := config.Parse([]byte(Header + body))
	if err != nil {
		t.Fatal(err)
	}
	if err := config.Validate(cfg); err != nil {
		t.Fatal(err)
	}
	return &cfg.Spec
}

// Fake starts the fake and returns it with a client on it. The fake serves the newest vendored spec as its own and
// checks every request and answer against each spec of the support window: the test fails on any problem.
func Fake(t *testing.T) (*logtofake.Server, *client.Client) {
	t.Helper()
	fake := logtofake.New("boot", "boot-secret")
	raw, err := logtospecs.Spec(logtospecs.Versions()[0])
	if err != nil {
		t.Fatal(err)
	}
	fake.ServeSpec(raw)
	docs, err := contract.Docs()
	if err != nil {
		t.Fatal(err)
	}
	fake.ValidateAgainst(docs)
	t.Cleanup(func() {
		for _, p := range fake.Problems() {
			t.Errorf("not valid for every supported Logto: %s", p)
		}
	})
	srv := httptest.NewServer(fake.Handler())
	t.Cleanup(srv.Close)
	return fake, client.New(client.Config{Endpoint: srv.URL, ClientID: "boot", ClientSecret: "boot-secret"})
}

// Env is a fresh environment for one run, with the target's own spec for the capability check (the fake serves the
// newest vendored one). A target that serves none leaves Capability nil, which the engine refuses.
func Env(c *client.Client, spec *v1alpha1.LogtoConfigSpec) *engine.Env {
	target, _ := capability.Fetch(context.Background(), c)
	return &engine.Env{Client: c, Spec: spec, Secrets: secrets.CLI(&secrets.Redactor{}), Capability: target}
}

// Apply runs the kinds on a fresh environment and fails the test on error.
func Apply(t *testing.T, c *client.Client, spec *v1alpha1.LogtoConfigSpec, kinds ...engine.Kind) *plan.Plan {
	t.Helper()
	p, err := run(c, spec, true, kinds...)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// Plan plans the kinds on a fresh environment.
func Plan(c *client.Client, spec *v1alpha1.LogtoConfigSpec, kinds ...engine.Kind) (*plan.Plan, error) {
	return run(c, spec, false, kinds...)
}

func run(c *client.Client, spec *v1alpha1.LogtoConfigSpec, apply bool, kinds ...engine.Kind) (*plan.Plan, error) {
	e, err := engine.New(kinds...)
	if err != nil {
		return nil, err
	}
	env := Env(c, spec)
	if apply {
		return e.Apply(context.Background(), env)
	}
	return e.Plan(context.Background(), env)
}

// Unchanged fails the test when a plan writes anything.
func Unchanged(t *testing.T, p *plan.Plan) {
	t.Helper()
	if p.Changed() {
		var b stringsBuilder
		_ = plan.Render(&b, p)
		t.Fatalf("the plan is not empty:\n%s", b.String())
	}
}

type stringsBuilder struct{ b []byte }

func (s *stringsBuilder) Write(p []byte) (int, error) { s.b = append(s.b, p...); return len(p), nil }
func (s *stringsBuilder) String() string              { return string(s.b) }

// JSON is v encoded, for comparisons in tests.
func JSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
