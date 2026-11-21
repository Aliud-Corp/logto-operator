//go:build integration

package profilefields_test

import (
	"strings"
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindtest"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/profilefields"
)

// On a real Logto: created, a replay plans nothing (Logto's defaults in config do not look like drift), a changed
// label is applied.
func TestOnARealLogto(t *testing.T) {
	c := kindtest.Real(t)
	u := kindtest.Unique(t)
	doc := strings.NewReplacer("name: fullname", "name: fullname"+u, "name: company", "name: company"+u).Replace(declared)
	kindtest.Apply(t, c, kindtest.Spec(t, doc), profilefields.Kind{})
	p, err := kindtest.Plan(c, kindtest.Spec(t, doc), profilefields.Kind{})
	if err != nil {
		t.Fatal(err)
	}
	kindtest.Unchanged(t, p)

	changed := strings.Replace(doc, "label: Company", "label: Employer", 1)
	kindtest.Apply(t, c, kindtest.Spec(t, changed), profilefields.Kind{})
	p, err = kindtest.Plan(c, kindtest.Spec(t, changed), profilefields.Kind{})
	if err != nil {
		t.Fatal(err)
	}
	kindtest.Unchanged(t, p)
}
