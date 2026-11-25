package applications_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Aliud-Corp/logto-operator/api/v1alpha1"
	"github.com/Aliud-Corp/logto-operator/internal/config"
	"github.com/Aliud-Corp/logto-operator/internal/engine"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/applications"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindtest"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/resources"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/roles"
	"github.com/Aliud-Corp/logto-operator/internal/engine/plan"
	"github.com/Aliud-Corp/logto-operator/internal/logto/client"
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

// A role declared in spec.roles is created and granted by one run, plan included: the plan cannot know its id yet.
func TestARoleCreatedByTheSameDocumentIsGranted(t *testing.T) {
	fake, c := kindtest.Fake(t)
	doc := strings.Replace(declared, "        - {name: Logto Management API access}\n", "        - {name: Logto Management API access}\n        - {name: event_sync}\n", 1) +
		"  roles:\n    sync: {name: event_sync, description: Syncs events, type: MachineToMachine}\n"
	spec := kindtest.Spec(t, doc)
	p, err := kindtest.Plan(c, spec, resources.Kind{}, roles.Kind{}, applications.Kind{})
	if err != nil {
		t.Fatal(err)
	}
	granted := false
	for _, op := range p.Ops {
		for _, ch := range op.Changes {
			granted = granted || (op.Key == "backend" && ch.Field == "roles.event_sync")
		}
	}
	if !granted {
		t.Fatalf("the plan does not grant event_sync: %v", p.Ops)
	}
	apply(t, fake, kindtest.Env(c, spec))
	var backendID string
	for _, a := range fake.Applications() {
		if a["name"] == "Example Backend" {
			backendID = a["id"].(string)
		}
	}
	if got := strings.Join(fake.ApplicationRoleNames(backendID), ","); got != logtofake.ManagementRole+",event_sync" {
		t.Fatalf("roles = %q", got)
	}
	writes := fake.Writes()
	apply(t, fake, kindtest.Env(c, spec))
	if fake.Writes() != writes {
		t.Fatalf("the replay wrote %d times", fake.Writes()-writes)
	}
}

// Prune never deletes a role the document grants, declared in spec.roles or not.
func TestPruneSparesARoleTheDocumentGrants(t *testing.T) {
	fake, c := kindtest.Fake(t)
	fake.SeedRole("ops_sync", "MachineToMachine")
	doc := strings.Replace(declared, "        - {name: Logto Management API access}\n", "        - {name: Logto Management API access}\n        - {name: ops_sync}\n", 1) +
		"  prune: {roles: true}\n"
	apply(t, fake, kindtest.Env(c, kindtest.Spec(t, doc)))
	if _, ok := fake.Roles()["ops_sync"]; !ok {
		t.Fatal("prune deleted ops_sync, which the document grants")
	}
}

const thirdParty = `  resources:
    mcp:
      name: Example MCP
      indicator: https://mcp.example.test
      scopes:
        read: {}
        write: {}
  applications:
    connector:
      name: Example Connector
      type: Traditional
      isThirdParty: true
      oidcClientMetadata:
        redirectUris: [https://client.example.test/callback]
      consentScopes:
        resources:
          - {resource: mcp, scope: read}
        user: [profile, email]
`

func applyAll(t *testing.T, c *client.Client, spec *v1alpha1.LogtoConfigSpec) *plan.Plan {
	t.Helper()
	return kindtest.Apply(t, c, spec, resources.Kind{}, roles.Kind{}, applications.Kind{})
}

func connectorID(t *testing.T, fake *logtofake.Server) string {
	t.Helper()
	for _, a := range fake.Applications() {
		if a["name"] == "Example Connector" {
			return a["id"].(string)
		}
	}
	t.Fatal("no Example Connector")
	return ""
}

func TestAThirdPartyApplicationIsCreatedWithItsConsentScopesAndAReplayWritesNothing(t *testing.T) {
	fake, c := kindtest.Fake(t)
	spec := kindtest.Spec(t, thirdParty)
	applyAll(t, c, spec)

	id := connectorID(t, fake)
	var app map[string]any
	for _, a := range fake.Applications() {
		if a["id"] == id {
			app = a
		}
	}
	if app["isThirdParty"] != true {
		t.Fatalf("the application is not third-party: %v", app)
	}
	got := fake.ApplicationConsentScopes(id)
	if strings.Join(got.Resources, ",") != "https://mcp.example.test read" || strings.Join(got.User, ",") != "email,profile" {
		t.Fatalf("consent scopes = %+v", got)
	}

	writes := fake.Writes()
	p := applyAll(t, c, spec)
	kindtest.Unchanged(t, p)
	if fake.Writes() != writes {
		t.Fatalf("the replay wrote %d times", fake.Writes()-writes)
	}
}

func TestConsentScopesAreExactWhenDeclaredAndLeftAloneWhenOmitted(t *testing.T) {
	fake, c := kindtest.Fake(t)
	applyAll(t, c, kindtest.Spec(t, thirdParty))
	id := connectorID(t, fake)

	// Swap the resource scope, drop a user scope.
	changed := strings.NewReplacer("{resource: mcp, scope: read}", "{resource: mcp, scope: write}", "[profile, email]", "[profile]").Replace(thirdParty)
	spec := kindtest.Spec(t, changed)
	p, err := kindtest.Plan(c, spec, resources.Kind{}, roles.Kind{}, applications.Kind{})
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]bool{}
	for _, op := range p.Ops {
		for _, ch := range op.Changes {
			fields[ch.Field] = true
		}
	}
	if !fields["consentScopes.resources"] || !fields["consentScopes.user"] {
		t.Fatalf("the plan does not show the consent changes: %v", p.Ops)
	}
	applyAll(t, c, spec)
	got := fake.ApplicationConsentScopes(id)
	if strings.Join(got.Resources, ",") != "https://mcp.example.test write" || strings.Join(got.User, ",") != "profile" {
		t.Fatalf("consent scopes = %+v", got)
	}
	writes := fake.Writes()
	kindtest.Unchanged(t, applyAll(t, c, spec))
	if fake.Writes() != writes {
		t.Fatal("the replay wrote")
	}

	// Omitted: left as Logto has them. An empty list: removed.
	omitted := strings.Replace(thirdParty, "      consentScopes:\n        resources:\n          - {resource: mcp, scope: read}\n        user: [profile, email]\n", "", 1)
	applyAll(t, c, kindtest.Spec(t, omitted))
	if got := fake.ApplicationConsentScopes(id); len(got.Resources) != 1 || len(got.User) != 1 {
		t.Fatalf("omitting consentScopes changed them: %+v", got)
	}
	cleared := strings.Replace(thirdParty, "        resources:\n          - {resource: mcp, scope: read}\n        user: [profile, email]\n", "        resources: []\n        user: []\n", 1)
	applyAll(t, c, kindtest.Spec(t, cleared))
	if got := fake.ApplicationConsentScopes(id); len(got.Resources) != 0 || len(got.User) != 0 {
		t.Fatalf("empty lists did not remove the scopes: %+v", got)
	}
}

func TestChangingIsThirdPartyIsRefused(t *testing.T) {
	fake, c := kindtest.Fake(t)
	fake.SeedApplication(logtofake.Application{Name: "Example Connector", Type: "Traditional"})
	_, err := kindtest.Plan(c, kindtest.Spec(t, thirdParty), resources.Kind{}, roles.Kind{}, applications.Kind{})
	if err == nil || !strings.Contains(err.Error(), "Logto sets it at creation only") {
		t.Fatalf("first-party made third-party: err = %v", err)
	}

	fake2, c2 := kindtest.Fake(t)
	fake2.SeedApplication(logtofake.Application{Name: "Example Connector", Type: "Traditional", ThirdParty: true})
	first := strings.Replace(thirdParty, "isThirdParty: true", "isThirdParty: false", 1)
	first = strings.Replace(first, "      consentScopes:\n        resources:\n          - {resource: mcp, scope: read}\n        user: [profile, email]\n", "", 1)
	_, err = kindtest.Plan(c2, kindtest.Spec(t, first), resources.Kind{}, roles.Kind{}, applications.Kind{})
	if err == nil || !strings.Contains(err.Error(), "Logto sets it at creation only") {
		t.Fatalf("third-party made first-party: err = %v", err)
	}
}

func TestConsentScopesOnAFirstPartyApplicationAreRefused(t *testing.T) {
	doc := strings.Replace(thirdParty, "      isThirdParty: true\n", "", 1)
	cfg, err := config.Parse([]byte(kindtest.Header + doc))
	if err != nil {
		t.Fatal(err)
	}
	err = config.Validate(cfg)
	if err == nil || !strings.Contains(err.Error(), "spec.applications.connector.consentScopes: applies to a third-party application only") {
		t.Fatalf("err = %v", err)
	}
}
