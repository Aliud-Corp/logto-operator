//go:build integration

package connectors_test

import (
	"strings"
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/connectors"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindtest"
)

// On a real Logto: created, a replay plans nothing (Logto's stored config compares equal), a changed password is
// drift and is applied with the whole config.
func TestOnARealLogto(t *testing.T) {
	t.Setenv("TEST_SMTP_PASS", "first-password")
	c := kindtest.Real(t)
	doc := strings.Replace(declared, "id: example-smtp", "id: it-smtp-"+kindtest.Unique(t), 1)
	kindtest.Apply(t, c, kindtest.Spec(t, doc), connectors.Kind{})
	p, err := kindtest.Plan(c, kindtest.Spec(t, doc), connectors.Kind{})
	if err != nil {
		t.Fatal(err)
	}
	kindtest.Unchanged(t, p)

	t.Setenv("TEST_SMTP_PASS", "second-password")
	p, err = kindtest.Plan(c, kindtest.Spec(t, doc), connectors.Kind{})
	if err != nil || !p.Changed() {
		t.Fatalf("a changed password must be drift: changed=%v err=%v", p != nil && p.Changed(), err)
	}
	kindtest.Apply(t, c, kindtest.Spec(t, doc), connectors.Kind{})
	p, err = kindtest.Plan(c, kindtest.Spec(t, doc), connectors.Kind{})
	if err != nil {
		t.Fatal(err)
	}
	kindtest.Unchanged(t, p)
}
