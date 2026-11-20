package logtofake_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/logto/logtofake"
	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

// The hello-world of the chain: the GENERATED client talks to the fake built on the GENERATED server, and decodes
// Logto's answer into the generated types.
func TestTheGeneratedClientListsApplicationsFromTheFake(t *testing.T) {
	fake := logtofake.New("boot", "boot-secret")
	fake.SeedApplication(logtofake.Application{ID: "app1", Name: "Example App", Type: "SPA"})
	srv := httptest.NewServer(fake.Handler())
	t.Cleanup(srv.Close)

	client := clientFor(t, srv.URL, "boot", "boot-secret")
	res, err := client.ListApplicationsWithResponse(context.Background(), &mgmt.ListApplicationsParams{})
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode() != http.StatusOK || res.JSON200 == nil {
		t.Fatalf("status %d, body %s", res.StatusCode(), res.Body)
	}
	apps := *res.JSON200
	if len(apps) != 1 || apps[0].Id != "app1" || apps[0].Name != "Example App" || string(apps[0].Type) != "SPA" {
		t.Fatalf("applications = %+v", apps)
	}
}

func TestARouteTheFakeDoesNotModelAnswers501(t *testing.T) {
	fake := logtofake.New("boot", "boot-secret")
	srv := httptest.NewServer(fake.Handler())
	t.Cleanup(srv.Close)

	res, err := clientFor(t, srv.URL, "boot", "boot-secret").ListHooksWithResponse(context.Background(), &mgmt.ListHooksParams{})
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode() != http.StatusNotImplemented {
		t.Fatalf("status %d, want 501: an unmodelled route must never look like a success", res.StatusCode())
	}
}

func TestTheFakeRefusesAWrongSecret(t *testing.T) {
	fake := logtofake.New("boot", "boot-secret")
	srv := httptest.NewServer(fake.Handler())
	t.Cleanup(srv.Close)

	if status := tokenStatus(t, srv.URL, "boot", "nope"); status != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", status)
	}
}

func clientFor(t *testing.T, endpoint, id, secret string) *mgmt.ClientWithResponses {
	t.Helper()
	token := fetchToken(t, endpoint, id, secret)
	c, err := mgmt.NewClientWithResponses(endpoint, mgmt.WithRequestEditorFn(func(_ context.Context, r *http.Request) error {
		r.Header.Set("Authorization", "Bearer "+token)
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func tokenRequest(t *testing.T, endpoint, id, secret string) *http.Response {
	t.Helper()
	form := url.Values{"grant_type": {"client_credentials"}, "resource": {"https://default.logto.app/api"}, "scope": {"all"}}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint+"/oidc/token", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(id, secret)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func tokenStatus(t *testing.T, endpoint, id, secret string) int {
	t.Helper()
	res := tokenRequest(t, endpoint, id, secret)
	defer func() { _ = res.Body.Close() }()
	return res.StatusCode
}

func fetchToken(t *testing.T, endpoint, id, secret string) string {
	t.Helper()
	res := tokenRequest(t, endpoint, id, secret)
	defer func() { _ = res.Body.Close() }()
	var body struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil || body.AccessToken == "" {
		t.Fatalf("no token (status %d): %v", res.StatusCode, err)
	}
	return body.AccessToken
}
