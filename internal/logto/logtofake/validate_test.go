package logtofake_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/logto/client"
	"github.com/Aliud-Corp/logto-operator/internal/logto/contract"
	"github.com/Aliud-Corp/logto-operator/internal/logto/logtofake"
	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

func validating(t *testing.T) (*logtofake.Server, *client.Client) {
	t.Helper()
	fake := logtofake.New("boot", "boot-secret")
	docs, err := contract.Docs()
	if err != nil {
		t.Fatal(err)
	}
	fake.ValidateAgainst(docs)
	srv := httptest.NewServer(fake.Handler())
	t.Cleanup(srv.Close)
	return fake, client.New(client.Config{Endpoint: srv.URL, ClientID: "boot", ClientSecret: "boot-secret"})
}

func TestABodyInvalidForASupportedLogtoIsRefusedAndReported(t *testing.T) {
	fake, c := validating(t)
	req, _ := mgmt.NewCreateApplicationRequestWithBody(c.Endpoint(), "application/json", strings.NewReader(`{"name":"a","type":"SPA","notAField":true}`))
	err := c.Do(context.Background(), req, nil)
	if !client.IsStatus(err, http.StatusBadRequest) {
		t.Fatalf("err = %v, want 400", err)
	}
	p := fake.Problems()
	if len(p) != 1 || !strings.Contains(p[0], "POST /api/applications") || !strings.Contains(p[0], "notAField") || !strings.Contains(p[0], "1.44.0") {
		t.Fatalf("problems = %v", p)
	}
}

func TestAValidBodyAndItsAnswerLeaveNoProblem(t *testing.T) {
	fake, c := validating(t)
	req, _ := mgmt.NewCreateApplicationRequestWithBody(c.Endpoint(), "application/json", strings.NewReader(`{"name":"a","type":"SPA"}`))
	if err := c.Do(context.Background(), req, nil); err != nil {
		t.Fatal(err)
	}
	req, _ = mgmt.NewGetSignInExpRequest(c.Endpoint())
	if err := c.Do(context.Background(), req, nil); err != nil {
		t.Fatal(err)
	}
	if p := fake.Problems(); len(p) != 0 {
		t.Fatalf("problems = %v", p)
	}
}

func TestAnAnswerOfTheWrongTypeIsReported(t *testing.T) {
	fake, c := validating(t)
	fake.SeedApplication(logtofake.Application{Name: "a", Type: "NotAnApplicationType"})
	req, _ := mgmt.NewListApplicationsRequest(c.Endpoint(), &mgmt.ListApplicationsParams{})
	if err := c.Do(context.Background(), req, nil); err != nil {
		t.Fatal(err)
	}
	p := fake.Problems()
	if len(p) != 1 || !strings.Contains(p[0], "answer GET /api/applications 200") || !strings.Contains(p[0], "type") {
		t.Fatalf("problems = %v", p)
	}
}

func TestARouteOneSupportedVersionLacksIsRefused(t *testing.T) {
	fake, c := validating(t)
	req, _ := mgmt.NewGetSystemLicenseRequest(c.Endpoint())
	err := c.Do(context.Background(), req, nil)
	if !client.IsStatus(err, http.StatusBadRequest) {
		t.Fatalf("err = %v, want 400", err)
	}
	if p := fake.Problems(); len(p) != 1 || !strings.Contains(p[0], "1.44.0: no such operation") {
		t.Fatalf("problems = %v", p)
	}
}
