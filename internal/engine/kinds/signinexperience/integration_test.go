//go:build integration

package signinexperience_test

import (
	"strings"
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindtest"
)

// On a real Logto: the connector then the declared keys are applied, and a replay plans nothing (what Logto stores
// compares equal to what was sent).
func TestOnARealLogto(t *testing.T) {
	t.Setenv("TEST_SMTP_PASS", "pass-value")
	c := kindtest.Real(t)
	doc := strings.Replace(smtp, "id: example-smtp", "id: it-sie-"+kindtest.Unique(t), 1) + sie
	kindtest.Apply(t, c, kindtest.Spec(t, doc), kinds...)
	p, err := kindtest.Plan(c, kindtest.Spec(t, doc), kinds...)
	if err != nil {
		t.Fatal(err)
	}
	kindtest.Unchanged(t, p)
}
