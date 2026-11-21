package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Aliud-Corp/logto-operator/internal/logto/client"
	"github.com/Aliud-Corp/logto-operator/internal/logto/logtofake"
	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

func setup(t *testing.T) (*logtofake.Server, *client.Client) {
	t.Helper()
	fake := logtofake.New("boot", "boot-secret")
	srv := httptest.NewServer(fake.Handler())
	t.Cleanup(srv.Close)
	c := client.New(client.Config{Endpoint: srv.URL, ClientID: "boot", ClientSecret: "boot-secret", RetryBase: time.Millisecond})
	return fake, c
}

func listApplications(ctx context.Context, c *client.Client) ([]struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}, error,
) {
	type app = struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	return client.List[app](ctx, c, func(page, size int) (*http.Request, error) {
		return mgmt.NewListApplicationsRequest(c.Endpoint(), &mgmt.ListApplicationsParams{Page: &page, PageSize: &size})
	})
}

func TestListReadsEveryPageOfAtMost100(t *testing.T) {
	fake, c := setup(t)
	for range 250 {
		fake.SeedApplication(logtofake.Application{Name: "a", Type: "SPA"})
	}
	apps, err := listApplications(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 250 {
		t.Fatalf("%d applications, want 250", len(apps))
	}
}

func TestAnExpiredTokenIsRefreshedTransparently(t *testing.T) {
	fake, c := setup(t)
	fake.SetTokenTTL(time.Second)
	if _, err := listApplications(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	if _, err := listApplications(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if n := fake.TokensIssued(); n != 2 {
		t.Fatalf("%d tokens issued, want 2 (one refresh)", n)
	}
}

func TestTransientErrorsAreRetried(t *testing.T) {
	fake, c := setup(t)
	fake.FailNext("/api/applications", http.StatusServiceUnavailable, http.StatusServiceUnavailable)
	if _, err := listApplications(context.Background(), c); err != nil {
		t.Fatalf("two 503 then 200 must succeed: %v", err)
	}
}

func TestRetriesAreBounded(t *testing.T) {
	fake, c := setup(t)
	fake.FailNext("/api/applications", 503, 503, 503, 503, 503)
	_, err := listApplications(context.Background(), c)
	if !client.IsStatus(err, 503) {
		t.Fatalf("err = %v, want the last 503", err)
	}
}

func TestACreationIsNeverReplayedOnAServerError(t *testing.T) {
	fake, c := setup(t)
	fake.FailNext("/api/applications", http.StatusBadGateway)
	req, _ := mgmt.NewCreateApplicationRequestWithBody(c.Endpoint(), "application/json", strings.NewReader(`{"name":"a","type":"SPA"}`))
	err := c.Do(context.Background(), req, nil)
	if !client.IsStatus(err, http.StatusBadGateway) {
		t.Fatalf("err = %v, want the 502: Logto may have created the object already", err)
	}
	if n := len(fake.Applications()); n != 0 {
		t.Fatalf("%d applications created by a replay", n)
	}
}

func TestACreationIsReplayedOnTooManyRequests(t *testing.T) {
	fake, c := setup(t)
	fake.FailNext("/api/applications", http.StatusTooManyRequests)
	req, _ := mgmt.NewCreateApplicationRequestWithBody(c.Endpoint(), "application/json", strings.NewReader(`{"name":"a","type":"SPA"}`))
	if err := c.Do(context.Background(), req, nil); err != nil {
		t.Fatalf("a 429 means not processed, the POST must be replayed: %v", err)
	}
	if n := len(fake.Applications()); n != 1 {
		t.Fatalf("%d applications, want 1", n)
	}
}

func TestAnErrorNeverCarriesTheRequestBody(t *testing.T) {
	fake, c := setup(t)
	fake.FailNext("/api/sign-in-exp", http.StatusBadRequest)
	req, _ := mgmt.NewUpdateSignInExpRequestWithBody(c.Endpoint(), &mgmt.UpdateSignInExpParams{}, "application/json", strings.NewReader(`{"password":"s3cr3t-value"}`))
	err := c.Do(context.Background(), req, nil)
	if err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(err.Error(), "s3cr3t-value") {
		t.Fatalf("the error carries the request body: %v", err)
	}
	if !strings.Contains(err.Error(), "PATCH /api/sign-in-exp: Logto answered 400 fake.injected") {
		t.Fatalf("err = %v", err)
	}
}

func TestAWrongSecretIsAnAuthErrorWithoutTheSecret(t *testing.T) {
	fake := logtofake.New("boot", "boot-secret")
	srv := httptest.NewServer(fake.Handler())
	t.Cleanup(srv.Close)
	c := client.New(client.Config{Endpoint: srv.URL, ClientID: "boot", ClientSecret: "wrong-secret-value"})
	_, err := listApplications(context.Background(), c)
	if err == nil || !strings.Contains(err.Error(), "check the credentials") || strings.Contains(err.Error(), "wrong-secret-value") {
		t.Fatalf("err = %v", err)
	}
}
