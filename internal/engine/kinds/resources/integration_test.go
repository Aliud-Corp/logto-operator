//go:build integration

package resources_test

import (
	"strings"
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindtest"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/resources"
)

// On a real Logto: created with its scopes, a replay plans nothing, a scope removed from the document is refused
// without prune and removed with it.
func TestOnARealLogto(t *testing.T) {
	c := kindtest.Real(t)
	u := kindtest.Unique(t)
	doc := strings.ReplaceAll(`  resources:
    app:
      name: IT App API UNIQ
      indicator: https://it-UNIQ.example.test/api
      accessTokenTtl: 7200
      scopes:
        "event:read": {description: Read events}
        "event:write": {description: Write events}
`, "UNIQ", u)
	kindtest.Apply(t, c, kindtest.Spec(t, doc), resources.Kind{})
	p, err := kindtest.Plan(c, kindtest.Spec(t, doc), resources.Kind{})
	if err != nil {
		t.Fatal(err)
	}
	kindtest.Unchanged(t, p)

	fewer := strings.Replace(doc, "        \"event:write\": {description: Write events}\n", "", 1)
	if _, err := kindtest.Plan(c, kindtest.Spec(t, fewer), resources.Kind{}); err == nil || !strings.Contains(err.Error(), "revokes it from every token") {
		t.Fatalf("err = %v", err)
	}
	withPrune := strings.Replace(fewer, "  resources:\n", "  prune: {resources: true}\n  resources:\n", 1)
	// Prune only this test's objects: every other object of the shared Logto is kept by name.
	kindtest.Apply(t, c, kindtest.KeepAllBut(t, c, kindtest.Spec(t, withPrune), u), resources.Kind{})
	p, err = kindtest.Plan(c, kindtest.Spec(t, fewer), resources.Kind{})
	if err != nil {
		t.Fatalf("after prune, the document without event:write plans: %v", err)
	}
	kindtest.Unchanged(t, p)
}
