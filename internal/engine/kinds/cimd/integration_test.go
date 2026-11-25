//go:build integration

package cimd_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/engine"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindtest"
)

// On a real Logto: the config and the consent ceiling are applied, and a replay plans nothing. CIMD is a tenant
// setting of the throwaway Logto, so the test turns it off again.
func TestOnARealLogto(t *testing.T) {
	c := kindtest.Real(t)
	u := kindtest.Unique(t)
	body := strings.NewReplacer("name: Example MCP", "name: IT MCP "+u, "https://mcp.example.test", "https://mcp-"+u+".example.test").Replace(doc)
	off := "  cimd: {enabled: false, consentScopes: {resources: [], user: []}}\n"
	t.Cleanup(func() { kindtest.Apply(t, c, kindtest.Spec(t, off), kinds...) })

	e, err := engine.New(kinds...)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Apply(context.Background(), kindtest.Env(c, kindtest.Spec(t, body))); err != nil {
		t.Fatal(err)
	}
	p, err := e.Apply(context.Background(), kindtest.Env(c, kindtest.Spec(t, body)))
	if err != nil {
		t.Fatal(err)
	}
	kindtest.Unchanged(t, p)
}
