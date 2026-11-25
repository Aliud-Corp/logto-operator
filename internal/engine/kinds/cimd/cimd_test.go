package cimd_test

import (
	"strings"
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/engine"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/cimd"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindtest"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/resources"
	"github.com/Aliud-Corp/logto-operator/internal/engine/plan"
)

const doc = `  resources:
    mcp:
      name: Example MCP
      indicator: https://mcp.example.test
      scopes:
        read: {}
        write: {}
  cimd:
    enabled: true
    addConsentPromptForOfflineAccess: true
    consentScopes:
      resources:
        - {resource: mcp, scope: read}
      user: [profile, email]
`

var kinds = []engine.Kind{resources.Kind{}, cimd.Kind{}}

func TestTheConfigAndTheConsentCeilingAreAppliedAndAReplayWritesNothing(t *testing.T) {
	fake, c := kindtest.Fake(t)
	spec := kindtest.Spec(t, doc)
	p := kindtest.Apply(t, c, spec, kinds...)

	fields := changedFields(p)
	for _, f := range []string{"enabled", "addConsentPromptForOfflineAccess", "consentScopes.resources", "consentScopes.user"} {
		if !fields[f] {
			t.Errorf("the plan does not name %s: %v", f, fields)
		}
	}
	got := fake.CIMD()
	if got["enabled"] != true || got["addConsentPromptForOfflineAccess"] != true {
		t.Fatalf("config = %v", got)
	}
	consent := fake.CIMDConsentScopes()
	if strings.Join(consent.Resources, ",") != "https://mcp.example.test read" || strings.Join(consent.User, ",") != "email,profile" {
		t.Fatalf("consent scopes = %+v", consent)
	}

	writes := fake.Writes()
	kindtest.Unchanged(t, kindtest.Apply(t, c, spec, kinds...))
	if fake.Writes() != writes {
		t.Fatalf("the replay wrote %d times", fake.Writes()-writes)
	}
}

func TestDeclaredListsAreExactAndOmittedOnesAreLeftAlone(t *testing.T) {
	fake, c := kindtest.Fake(t)
	kindtest.Apply(t, c, kindtest.Spec(t, doc), kinds...)

	changed := strings.NewReplacer("{resource: mcp, scope: read}", "{resource: mcp, scope: write}", "[profile, email]", "[profile]").Replace(doc)
	kindtest.Apply(t, c, kindtest.Spec(t, changed), kinds...)
	got := fake.CIMDConsentScopes()
	if strings.Join(got.Resources, ",") != "https://mcp.example.test write" || strings.Join(got.User, ",") != "profile" {
		t.Fatalf("consent scopes = %+v", got)
	}

	// No consentScopes: left as Logto has them; no addConsentPromptForOfflineAccess: left as is.
	bare := "  resources:\n    mcp: {name: Example MCP, indicator: https://mcp.example.test, scopes: {read: {}, write: {}}}\n  cimd: {enabled: false}\n"
	kindtest.Apply(t, c, kindtest.Spec(t, bare), kinds...)
	if cfg := fake.CIMD(); cfg["enabled"] != false || cfg["addConsentPromptForOfflineAccess"] != true {
		t.Fatalf("config = %v", cfg)
	}
	if got := fake.CIMDConsentScopes(); len(got.Resources) != 1 || len(got.User) != 1 {
		t.Fatalf("omitting consentScopes changed them: %+v", got)
	}

	cleared := "  resources:\n    mcp: {name: Example MCP, indicator: https://mcp.example.test, scopes: {read: {}, write: {}}}\n  cimd: {enabled: false, consentScopes: {resources: [], user: []}}\n"
	kindtest.Apply(t, c, kindtest.Spec(t, cleared), kinds...)
	if got := fake.CIMDConsentScopes(); len(got.Resources) != 0 || len(got.User) != 0 {
		t.Fatalf("empty lists did not remove the scopes: %+v", got)
	}
}

func TestAbsentBlockIsNotManagedAtAll(t *testing.T) {
	fake, c := kindtest.Fake(t)
	// A read of either route would fail the run.
	fake.FailNext("/api/configs/cimd", 500)
	fake.FailNext("/api/cimd/user-consent-scopes", 500)
	p := kindtest.Apply(t, c, kindtest.Spec(t, "  resources:\n    mcp: {name: Example MCP, indicator: https://mcp.example.test}\n"), kinds...)
	for _, op := range p.Ops {
		if op.Kind == cimd.Name {
			t.Fatalf("an absent cimd block planned %v", op)
		}
	}
}

func TestLogtoRefusingCIMDIsReportedWithItsCause(t *testing.T) {
	fake, c := kindtest.Fake(t)
	fake.BlockCIMD()
	_, err := kindtest.Plan(c, kindtest.Spec(t, doc), kinds...)
	if err != nil {
		t.Fatalf("planning must not refuse: %v", err)
	}
	env := kindtest.Env(c, kindtest.Spec(t, doc))
	e, _ := engine.New(kinds...)
	_, err = e.Apply(t.Context(), env)
	if err == nil || !strings.Contains(err.Error(), "spec.cimd") || !strings.Contains(err.Error(), "SSRF") {
		t.Fatalf("err = %v", err)
	}
}

func changedFields(p *plan.Plan) map[string]bool {
	out := map[string]bool{}
	for _, op := range p.Ops {
		for _, ch := range op.Changes {
			out[ch.Field] = true
		}
	}
	return out
}
