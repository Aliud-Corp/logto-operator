package connectors

import (
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/logto/contract"
)

func requireValid(t *testing.T, what string, p contract.Problems, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	for v, problems := range p {
		t.Errorf("%s is invalid for Logto %s: %v", what, v, problems)
	}
}

func TestTheWireBodiesAreValidForTheWholeWindow(t *testing.T) {
	cfg := smtpConfig{
		Host: "smtp.example.test", Port: 587, Auth: smtpAuth{User: "u", Pass: "p"}, FromEmail: "a@example.test",
		Templates: []smtpTemplate{{UsageType: "Generic", ContentType: "text/plain", Subject: "s", Content: "c"}},
	}
	for _, body := range []createConnector{{ID: "example-smtp", ConnectorID: SMTPFactory, Config: cfg}, {ConnectorID: SMTPFactory, Config: cfg}} {
		p, err := contract.Request("CreateConnector", body)
		requireValid(t, "CreateConnector", p, err)
	}
	p, err := contract.Request("UpdateConnector", updateConnector{Config: cfg})
	requireValid(t, "UpdateConnector", p, err)
}
