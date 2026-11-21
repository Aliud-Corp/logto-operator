package resources_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindtest"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/resources"
	"github.com/Aliud-Corp/logto-operator/internal/engine/plan"
	"github.com/Aliud-Corp/logto-operator/internal/logto/client"
	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

const declared = `  resources:
    app:
      name: Example App API
      indicator: https://app.example.test/api
      accessTokenTtl: 7200
      scopes:
        "event:read": {description: Read events}
        "event:write": {description: Write events}
`

func TestAnEmptyLogtoGetsTheResourcesAndAReplayWritesNothing(t *testing.T) {
	fake, c := kindtest.Fake(t)
	spec := kindtest.Spec(t, declared)
	kindtest.Apply(t, c, spec, resources.Kind{})

	if got := fake.ResourceScopes("https://app.example.test/api"); strings.Join(got, ",") != "event:read,event:write" {
		t.Fatalf("scopes = %v", got)
	}
	writes := fake.Writes()
	kindtest.Unchanged(t, kindtest.Apply(t, c, spec, resources.Kind{}))
	if fake.Writes() != writes {
		t.Fatalf("the replay wrote %d times", fake.Writes()-writes)
	}
}

func createResource(t *testing.T, c *client.Client, body string) string {
	t.Helper()
	req, err := mgmt.NewCreateResourceRequestWithBody(c.Endpoint(), "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := c.Do(context.Background(), req, &out); err != nil {
		t.Fatal(err)
	}
	return out.ID
}

func TestAnExistingResourceIsAdoptedByIndicator(t *testing.T) {
	fake, c := kindtest.Fake(t)
	id := createResource(t, c, `{"name":"Old name","indicator":"https://app.example.test/api"}`)
	kindtest.Apply(t, c, kindtest.Spec(t, declared), resources.Kind{})
	var found int
	for _, r := range fake.Resources() {
		if r["indicator"] == "https://app.example.test/api" {
			found++
			if r["id"] != id || r["name"] != "Example App API" {
				t.Fatalf("not adopted: %v", r)
			}
		}
	}
	if found != 1 {
		t.Fatalf("%d resources with the indicator, want 1", found)
	}
}

func TestAnUndeclaredScopeIsRefusedWithoutPrune(t *testing.T) {
	_, c := kindtest.Fake(t)
	kindtest.Apply(t, c, kindtest.Spec(t, declared), resources.Kind{})
	fewer := strings.Replace(declared, "        \"event:write\": {description: Write events}\n", "", 1)
	_, err := kindtest.Plan(c, kindtest.Spec(t, fewer), resources.Kind{})
	if err == nil || !strings.Contains(err.Error(), `Logto holds the scope "event:write"`) || !strings.Contains(err.Error(), "revokes it from every token") {
		t.Fatalf("err = %v", err)
	}
}

func TestPruneRemovesUndeclaredScopesAndResourcesButNotTheManagementAPI(t *testing.T) {
	fake, c := kindtest.Fake(t)
	kindtest.Apply(t, c, kindtest.Spec(t, declared), resources.Kind{})
	createResource(t, c, `{"name":"Stray","indicator":"https://stray.example.test/api"}`)
	createResource(t, c, `{"name":"Kept by name","indicator":"https://kept-name.example.test/api"}`)
	createResource(t, c, `{"name":"Kept by indicator","indicator":"https://kept-indicator.example.test/api"}`)
	fewer := "  prune: {resources: true, keep: [Kept by name, \"https://kept-indicator.example.test/api\"]}\n" + strings.Replace(declared, "        \"event:write\": {description: Write events}\n", "", 1)
	kindtest.Apply(t, c, kindtest.Spec(t, fewer), resources.Kind{})

	if got := fake.ResourceScopes("https://app.example.test/api"); strings.Join(got, ",") != "event:read" {
		t.Fatalf("scopes = %v", got)
	}
	var indicators []string
	for _, r := range fake.Resources() {
		indicators = append(indicators, r["indicator"].(string))
	}
	if got := strings.Join(indicators, ","); strings.Contains(got, "stray") || !strings.Contains(got, "https://default.logto.app/api") ||
		!strings.Contains(got, "kept-name") || !strings.Contains(got, "kept-indicator") {
		t.Fatalf("resources = %s", got)
	}
}

func TestAChangedTTLIsUpdated(t *testing.T) {
	_, c := kindtest.Fake(t)
	kindtest.Apply(t, c, kindtest.Spec(t, declared), resources.Kind{})
	p, err := kindtest.Plan(c, kindtest.Spec(t, strings.Replace(declared, "7200", "3600", 1)), resources.Kind{})
	if err != nil {
		t.Fatal(err)
	}
	if p.Count(plan.Update) != 1 || len(p.Ops[0].Changes) != 1 || p.Ops[0].Changes[0].Field != "accessTokenTtl" {
		t.Fatalf("plan = %+v", p.Ops)
	}
}
