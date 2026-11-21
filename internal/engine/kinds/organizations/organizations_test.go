package organizations_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/engine"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindtest"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/organizations"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/resources"
	"github.com/Aliud-Corp/logto-operator/internal/engine/plan"
	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

const template = `  resources:
    app:
      name: Example App API
      indicator: https://app.example.test/api
      scopes:
        "event:read": {}
        "event:write": {}
  organizationScopes:
    "members:read": {description: Read members}
    "members:write": {description: Manage members}
  organizationRoles:
    owner:
      name: organization_owner
      description: Owns the organization
      organizationScopes: ["members:read", "members:write"]
      resourceScopes:
        - {resource: app, scope: "event:read"}
        - {resource: app, scope: "event:write"}
    member:
      name: organization_member
      organizationScopes: ["members:read"]
      resourceScopes:
        - {resource: app, scope: "event:read"}
`

var kinds = []engine.Kind{resources.Kind{}, organizations.Scopes{}, organizations.Roles{}}

func TestTheTemplateIsCreatedAndAReplayWritesNothing(t *testing.T) {
	fake, c := kindtest.Fake(t)
	spec := kindtest.Spec(t, template)
	kindtest.Apply(t, c, spec, kinds...)

	roles := fake.OrganizationRoles()
	owner := roles["organization_owner"]
	if strings.Join(owner.Scopes, ",") != "members:read,members:write" || len(owner.ResourceScopes) != 2 || owner.Description != "Owns the organization" {
		t.Fatalf("owner = %+v", owner)
	}
	if m := roles["organization_member"]; strings.Join(m.Scopes, ",") != "members:read" || strings.Join(m.ResourceScopes, ",") != "event:read" {
		t.Fatalf("member = %+v", m)
	}
	writes := fake.Writes()
	kindtest.Unchanged(t, kindtest.Apply(t, c, spec, kinds...))
	if fake.Writes() != writes {
		t.Fatalf("the replay wrote %d times", fake.Writes()-writes)
	}
}

func TestAPlanOnAnEmptyLogtoShowsScopeIdsAsKnownAfterApply(t *testing.T) {
	_, c := kindtest.Fake(t)
	p, err := kindtest.Plan(c, kindtest.Spec(t, template), kinds...)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	_ = plan.Render(&b, p)
	if !strings.Contains(b.String(), "event:read@"+plan.KnownAfterApply) {
		t.Fatalf("plan:\n%s", b.String())
	}
}

func TestARoleHoldsExactlyTheDeclaredScopes(t *testing.T) {
	fake, c := kindtest.Fake(t)
	kindtest.Apply(t, c, kindtest.Spec(t, template), kinds...)
	fewer := strings.Replace(template, `organizationScopes: ["members:read", "members:write"]`, `organizationScopes: ["members:write"]`, 1)
	fewer = strings.Replace(fewer, "        - {resource: app, scope: \"event:write\"}\n", "", 1)
	kindtest.Apply(t, c, kindtest.Spec(t, fewer), kinds...)
	owner := fake.OrganizationRoles()["organization_owner"]
	if strings.Join(owner.Scopes, ",") != "members:write" || strings.Join(owner.ResourceScopes, ",") != "event:read" {
		t.Fatalf("owner = %+v", owner)
	}
}

func TestAnUndeclaredRoleIsDeletedOnlyWithPrune(t *testing.T) {
	fake, c := kindtest.Fake(t)
	kindtest.Apply(t, c, kindtest.Spec(t, template), kinds...)
	req, _ := mgmt.NewCreateOrganizationRoleRequestWithBody(c.Endpoint(), "application/json",
		strings.NewReader(`{"name":"stray","organizationScopeIds":[],"resourceScopeIds":[]}`))
	if err := c.Do(context.Background(), req, nil); err != nil {
		t.Fatal(err)
	}
	kindtest.Apply(t, c, kindtest.Spec(t, template), kinds...)
	if _, ok := fake.OrganizationRoles()["stray"]; !ok {
		t.Fatal("an undeclared role was deleted without prune")
	}
	kindtest.Apply(t, c, kindtest.Spec(t, "  prune: {organizationRoles: true}\n"+template), kinds...)
	if _, ok := fake.OrganizationRoles()["stray"]; ok {
		t.Fatal("the undeclared role survived the prune")
	}
}
