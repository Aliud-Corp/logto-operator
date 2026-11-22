//go:build integration

package applications_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/engine"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/applications"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindtest"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/resources"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/roles"
	"github.com/Aliud-Corp/logto-operator/internal/logto/client"
)

// On a real Logto: a SPA and an M2M application with the Management API role; the published secret gets a
// Management API token at the tenant endpoint; a replay plans nothing and publishes the same secret.
func TestOnARealLogto(t *testing.T) {
	c := kindtest.Real(t)
	u := kindtest.Unique(t)
	doc := strings.NewReplacer("name: Example App", "name: IT App "+u, "name: Example Backend", "name: IT Backend "+u).Replace(declared)
	e, err := engine.New(resources.Kind{}, roles.Kind{}, applications.Kind{})
	if err != nil {
		t.Fatal(err)
	}
	env := kindtest.Env(c, kindtest.Spec(t, doc))
	if _, err := e.Apply(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	id := env.Outputs.IDs[applications.OutputID("backend")]
	secret := env.Outputs.Secrets[applications.OutputSecret("backend")]
	if id == "" || secret.IsZero() {
		t.Fatalf("outputs = %v", env.Outputs.IDs)
	}
	m2m := client.New(client.Config{Endpoint: c.Endpoint(), ClientID: id, ClientSecret: secret.Reveal()})
	if _, err := m2m.Token(context.Background()); err != nil {
		t.Fatalf("the published M2M secret gets no Management API token: %v", err)
	}

	again := kindtest.Env(c, kindtest.Spec(t, doc))
	p, err := e.Apply(context.Background(), again)
	if err != nil {
		t.Fatal(err)
	}
	kindtest.Unchanged(t, p)
	if again.Outputs.Secrets[applications.OutputSecret("backend")].Reveal() != secret.Reveal() {
		t.Fatal("a replay published another secret")
	}
}
