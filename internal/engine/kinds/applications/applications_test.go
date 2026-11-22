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
	"github.com/Aliud-Corp/logto-operator/internal/logto/logtofake"
)

const declared = `  applications:
    app:
      name: Example App
      type: SPA
      description: The example application
      oidcClientMetadata:
        redirectUris: [https://app.example.test/callback]
        postLogoutRedirectUris: [https://app.example.test]
      customClientMetadata:
        corsAllowedOrigins: [https://app.example.test]
        alwaysIssueRefreshToken: true
    backend:
      name: Example Backend
      type: MachineToMachine
      roles:
        - {name: Logto Management API access}
      secret: {publish: true}
`

func apply(t *testing.T, fake *logtofake.Server, env *engine.Env) {
	t.Helper()
	e, err := engine.New(resources.Kind{}, roles.Kind{}, applications.Kind{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Apply(context.Background(), env); err != nil {
		t.Fatal(err)
	}
}

func TestApplicationsAreCreatedWithTheirSecretAndRoleAndAReplayWritesNothing(t *testing.T) {
	fake, c := kindtest.Fake(t)
	spec := kindtest.Spec(t, declared)
	env := kindtest.Env(c, spec)
	apply(t, fake, env)

	var backendID string
	for _, a := range fake.Applications() {
		if a["name"] == "Example Backend" {
			backendID = a["id"].(string)
		}
	}
	if got := strings.Join(fake.ApplicationRoleNames(backendID), ","); got != logtofake.ManagementRole {
		t.Fatalf("roles = %q", got)
	}
	if env.Outputs.IDs[applications.OutputID("backend")] != backendID || env.Outputs.IDs[applications.OutputID("app")] == "" {
		t.Fatalf("ids = %v", env.Outputs.IDs)
	}
	secret := env.Outputs.Secrets[applications.OutputSecret("backend")]
	if secret.Reveal() != fake.ApplicationSecrets(backendID)[0] {
		t.Fatal("the published secret is not the application's")
	}

	writes := fake.Writes()
	again := kindtest.Env(c, spec)
	apply(t, fake, again)
	if fake.Writes() != writes {
		t.Fatalf("the replay wrote %d times", fake.Writes()-writes)
	}
	if again.Outputs.Secrets[applications.OutputSecret("backend")].Reveal() != secret.Reveal() || len(fake.ApplicationSecrets(backendID)) != 1 {
		t.Fatal("a replay must publish the same secret and create none")
	}
}

func TestAnExistingApplicationIsAdoptedByName(t *testing.T) {
	fake, c := kindtest.Fake(t)
	fake.SeedApplication(logtofake.Application{ID: "seeded-app", Name: "Example App", Type: "SPA"})
	env := kindtest.Env(c, kindtest.Spec(t, declared))
	apply(t, fake, env)
	if env.Outputs.IDs[applications.OutputID("app")] != "seeded-app" {
		t.Fatalf("not adopted: %v", env.Outputs.IDs)
	}
	n := 0
	for _, a := range fake.Applications() {
		if a["name"] == "Example App" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d applications named Example App", n)
	}
}

func TestATypeChangeIsRefused(t *testing.T) {
	fake, c := kindtest.Fake(t)
	fake.SeedApplication(logtofake.Application{Name: "Example App", Type: "Traditional"})
	_, err := kindtest.Plan(c, kindtest.Spec(t, declared), resources.Kind{}, roles.Kind{}, applications.Kind{})
	if err == nil || !strings.Contains(err.Error(), "Logto cannot change an application's type") {
		t.Fatalf("err = %v", err)
	}
}

func TestADuplicateNameIsRefused(t *testing.T) {
	fake, c := kindtest.Fake(t)
	fake.SeedApplication(logtofake.Application{Name: "Example App", Type: "SPA"})
	fake.SeedApplication(logtofake.Application{Name: "Example App", Type: "SPA"})
	_, err := kindtest.Plan(c, kindtest.Spec(t, declared), resources.Kind{}, roles.Kind{}, applications.Kind{})
	if err == nil || !strings.Contains(err.Error(), `2 Logto objects match "Example App"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestAMissingRoleIsRefused(t *testing.T) {
	_, c := kindtest.Fake(t)
	doc := strings.Replace(declared, "Logto Management API access", "No such role", 1)
	_, err := kindtest.Plan(c, kindtest.Spec(t, doc), resources.Kind{}, roles.Kind{}, applications.Kind{})
	if err == nil || !strings.Contains(err.Error(), `Logto has no MachineToMachine role named "No such role"`) {
		t.Fatalf("err = %v", err)
	}
}

// A plan (and check) never holds a secret value: only an apply publishes it.
func TestAPlanPublishesNoSecret(t *testing.T) {
	fake, c := kindtest.Fake(t)
	spec := kindtest.Spec(t, declared)
	apply(t, fake, kindtest.Env(c, spec))
	env := kindtest.Env(c, spec)
	e, _ := engine.New(resources.Kind{}, roles.Kind{}, applications.Kind{})
	if _, err := e.Plan(context.Background(), env); err != nil {
		t.Fatal(err)
	}
	if len(env.Outputs.Secrets) != 0 {
		t.Fatalf("a plan published %d secret(s)", len(env.Outputs.Secrets))
	}
}
