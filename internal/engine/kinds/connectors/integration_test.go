//go:build integration

package connectors_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/connectors"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindtest"
	"github.com/Aliud-Corp/logto-operator/internal/logto/client"
	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
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

// On a real Logto: ignoreTLS turned off and replyTo removed reach Logto (the config is sent whole, over Logto's own),
// and a replay plans nothing.
func TestOnARealLogtoAFieldTurnedOffOrRemovedReachesLogto(t *testing.T) {
	t.Setenv("TEST_SMTP_PASS", "first-password")
	c := kindtest.Real(t)
	id := "it-smtp-" + kindtest.Unique(t)
	off := strings.Replace(declared, "id: example-smtp", "id: "+id, 1)
	on := strings.Replace(off, "        port: 587\n", "        port: 587\n        ignoreTLS: true\n        replyTo: support@example.test\n", 1)
	kindtest.Apply(t, c, kindtest.Spec(t, on), connectors.Kind{})
	if cfg := realConfig(t, c, id); cfg["ignoreTLS"] != true || cfg["replyTo"] != "support@example.test" {
		t.Fatalf("Logto holds %v", cfg)
	}
	kindtest.Apply(t, c, kindtest.Spec(t, off), connectors.Kind{})
	cfg := realConfig(t, c, id)
	if _, has := cfg["replyTo"]; cfg["ignoreTLS"] != false || has {
		t.Fatalf("Logto kept what the document no longer declares: ignoreTLS=%v replyTo present=%v", cfg["ignoreTLS"], has)
	}
	p, err := kindtest.Plan(c, kindtest.Spec(t, off), connectors.Kind{})
	if err != nil {
		t.Fatal(err)
	}
	kindtest.Unchanged(t, p)
}

func realConfig(t *testing.T, c *client.Client, id string) map[string]any {
	t.Helper()
	req, err := mgmt.NewGetConnectorRequest(c.Endpoint(), id)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Config map[string]any `json:"config"`
	}
	if err := c.Do(context.Background(), req, &out); err != nil {
		t.Fatal(err)
	}
	return out.Config
}
