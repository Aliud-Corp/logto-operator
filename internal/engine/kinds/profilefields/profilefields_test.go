package profilefields_test

import (
	"strings"
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindtest"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/profilefields"
	"github.com/Aliud-Corp/logto-operator/internal/engine/plan"
)

const declared = `  customProfileFields:
    fullname:
      name: fullname
      type: Fullname
      label: Full name
      required: true
      config:
        parts:
          - {enabled: true, name: givenName, type: Text, label: Given name, required: true}
          - {enabled: false, name: middleName, type: Text, label: Middle name, required: false}
          - {enabled: true, name: familyName, type: Text, label: Family name, required: true}
    company:
      name: company
      type: Text
      label: Company
`

func TestTheFieldsAreCreatedAndAReplayWritesNothing(t *testing.T) {
	fake, c := kindtest.Fake(t)
	spec := kindtest.Spec(t, declared)
	kindtest.Apply(t, c, spec, profilefields.Kind{})
	fields := fake.ProfileFields()
	if fields["fullname"]["type"] != "Fullname" || fields["company"]["required"] != false {
		t.Fatalf("fields = %v", fields)
	}
	writes := fake.Writes()
	kindtest.Unchanged(t, kindtest.Apply(t, c, spec, profilefields.Kind{}))
	if fake.Writes() != writes {
		t.Fatalf("the replay wrote %d times", fake.Writes()-writes)
	}
}

func TestAChangedLabelUpdatesOnlyThatField(t *testing.T) {
	fake, c := kindtest.Fake(t)
	kindtest.Apply(t, c, kindtest.Spec(t, declared), profilefields.Kind{})
	changed := strings.Replace(declared, "label: Company", "label: Employer", 1)
	p := kindtest.Apply(t, c, kindtest.Spec(t, changed), profilefields.Kind{})
	if p.Count(plan.Update) != 1 || p.Count(plan.Noop) != 1 {
		t.Fatalf("plan = %+v", p.Ops)
	}
	if fake.ProfileFields()["company"]["label"] != "Employer" {
		t.Fatal("the label was not updated")
	}
}

func TestAnUndeclaredFieldIsDeletedOnlyWithPrune(t *testing.T) {
	fake, c := kindtest.Fake(t)
	kindtest.Apply(t, c, kindtest.Spec(t, declared), profilefields.Kind{})
	onlyFullname := strings.Split(declared, "    company:\n")[0]
	kindtest.Apply(t, c, kindtest.Spec(t, onlyFullname), profilefields.Kind{})
	if _, ok := fake.ProfileFields()["company"]; !ok {
		t.Fatal("deleted without prune")
	}
	kindtest.Apply(t, c, kindtest.Spec(t, "  prune: {customProfileFields: true, keep: [company]}\n"+onlyFullname), profilefields.Kind{})
	if _, ok := fake.ProfileFields()["company"]; !ok {
		t.Fatal("a field listed in spec.prune.keep was pruned")
	}
	kindtest.Apply(t, c, kindtest.Spec(t, "  prune: {customProfileFields: true}\n"+onlyFullname), profilefields.Kind{})
	if _, ok := fake.ProfileFields()["company"]; ok {
		t.Fatal("survived the prune")
	}
}
