package plan_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/engine/plan"
)

func sample() *plan.Plan {
	p := &plan.Plan{}
	p.Add("resources",
		plan.Op{
			Kind: "resources", Key: "admin", Name: "Admin API", Action: plan.Update,
			Changes: []plan.Change{{Field: "accessTokenTtl", From: "3600", To: "7200"}},
		},
		plan.Op{
			Kind: "resources", Key: "app", Name: "App API", Action: plan.Create,
			Changes: []plan.Change{{Field: "indicator", To: "https://app.example.test/api"}},
		},
		plan.Op{Kind: "resources", Key: "kept", Action: plan.Noop},
	)
	p.Add("connectors",
		plan.Op{
			Kind: "connectors", Key: "smtp", Action: plan.Update,
			Changes: []plan.Change{{Field: "config.auth.pass", From: "old-secret", To: "new-secret", Sensitive: true}},
		},
	)
	p.Add("users")
	return p
}

func TestThePlanRendersLikeADiff(t *testing.T) {
	var out bytes.Buffer
	if err := plan.Render(&out, sample()); err != nil {
		t.Fatal(err)
	}
	want := `resources
  ~ admin (Admin API)
      accessTokenTtl: 3600 -> 7200
  + app (App API)
      indicator: https://app.example.test/api
  = 1 unchanged
connectors
  ~ smtp
      config.auth.pass: (sensitive) -> (sensitive)
users
Plan: 1 to create, 2 to update, 0 to delete, 1 unchanged.
`
	if out.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestRenderingIsStableAndNeverShowsASecret(t *testing.T) {
	var a, b bytes.Buffer
	_ = plan.Render(&a, sample())
	_ = plan.Render(&b, sample())
	if a.String() != b.String() {
		t.Fatal("two renderings of the same plan differ")
	}
	if strings.Contains(a.String(), "new-secret") || strings.Contains(a.String(), "old-secret") {
		t.Fatalf("a sensitive value is shown:\n%s", a.String())
	}
}

func TestAPlanOfNoopsChangesNothing(t *testing.T) {
	p := &plan.Plan{}
	p.Add("resources", plan.Op{Kind: "resources", Key: "a", Action: plan.Noop})
	if p.Changed() {
		t.Fatal("a plan of no-ops reports a change")
	}
	if !sample().Changed() {
		t.Fatal("a plan with updates reports no change")
	}
}

func TestSameSetIgnoresOrder(t *testing.T) {
	if !plan.SameSet([]string{"b", "a"}, []string{"a", "b"}) || plan.SameSet([]string{"a"}, []string{"a", "b"}) {
		t.Fatal("SameSet is wrong")
	}
}

// A sensitive change never holds its value once in a plan, whatever a kind put in it: a plan may be serialized (an
// operator's status, an event) without going through Render.
func TestASensitiveChangeKeepsNoValue(t *testing.T) {
	p := &plan.Plan{}
	p.Add("connectors", plan.Op{
		Kind: "connectors", Key: "smtp", Action: plan.Update,
		Changes: []plan.Change{{Field: "config.auth.pass", From: "old-password", To: "new-password", Sensitive: true}},
	})
	b, _ := json.Marshal(p)
	if strings.Contains(string(b), "old-password") || strings.Contains(string(b), "new-password") {
		t.Fatalf("the plan holds a sensitive value: %s", b)
	}
	if c := p.Ops[0].Changes[0]; c.From != plan.Masked || c.To != plan.Masked {
		t.Fatalf("change = %+v", c)
	}
}

// An op's payload carries the wire body, secrets included (a connector's password): it is never encoded nor printed.
func TestAnOpNeverShowsItsPayload(t *testing.T) {
	op := plan.Op{Kind: "connectors", Key: "smtp", Action: plan.Update, Payload: map[string]any{"pass": "payload-secret"}}
	p := &plan.Plan{}
	p.Add("connectors", op)
	b, _ := json.Marshal(p)
	for _, out := range []string{string(b), fmt.Sprintf("%v", op), fmt.Sprintf("%+v", op), fmt.Sprintf("%#v", op), fmt.Sprintf("%+v", p.Ops)} {
		if strings.Contains(out, "payload-secret") {
			t.Fatalf("the payload shows: %s", out)
		}
	}
	if !strings.Contains(fmt.Sprintf("%+v", op), "smtp") {
		t.Fatal("the op itself no longer prints")
	}
}
