//go:build integration

package organizations_test

import (
	"strings"
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindtest"
)

// On a real Logto: the template is created, a replay plans nothing, a role's scope set is replaced, an undeclared
// role (of this test) is pruned only with prune.organizationRoles.
func TestOnARealLogto(t *testing.T) {
	c := kindtest.Real(t)
	u := kindtest.Unique(t)
	doc := strings.NewReplacer(
		"https://app.example.test/api", "https://it-"+u+".example.test/api",
		"members:", "members-"+u+":",
		"organization_owner", "owner_"+u,
		"organization_member", "member_"+u,
		"Example App API", "IT "+u,
	).Replace(template)
	kindtest.Apply(t, c, kindtest.Spec(t, doc), kinds...)
	p, err := kindtest.Plan(c, kindtest.Spec(t, doc), kinds...)
	if err != nil {
		t.Fatal(err)
	}
	kindtest.Unchanged(t, p)

	fewer := strings.Replace(doc, "        - {resource: app, scope: \"event:write\"}\n", "", 1)
	kindtest.Apply(t, c, kindtest.Spec(t, fewer), kinds...)
	p, err = kindtest.Plan(c, kindtest.Spec(t, fewer), kinds...)
	if err != nil {
		t.Fatal(err)
	}
	kindtest.Unchanged(t, p)

	withoutMember := strings.Split(fewer, "    member:\n")[0]
	if p, err = kindtest.Plan(c, kindtest.Spec(t, withoutMember), kinds...); err != nil || p.Changed() {
		t.Fatalf("without prune, an undeclared role must be left alone: changed=%v err=%v", p != nil && p.Changed(), err)
	}
	pruned := kindtest.KeepAllBut(t, c, kindtest.Spec(t, "  prune: {organizationRoles: true}\n"+withoutMember), u)
	kindtest.Apply(t, c, pruned, kinds...)
	p, err = kindtest.Plan(c, kindtest.Spec(t, fewer), kinds...)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Changed() {
		t.Fatal("the member role was not pruned: re-declaring it plans nothing")
	}
}
