//go:build integration

package integration_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/logto/capability"
	"github.com/Aliud-Corp/logto-operator/internal/logto/client"
	"github.com/Aliud-Corp/logto-operator/internal/logto/integration"
	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

// The route the engine will use against a real Logto: token at the admin endpoint (m-default), Management API calls
// at the tenant endpoint, responses decoded by the generated client.
func TestTheHarnessReachesTheManagementAPI(t *testing.T) {
	env := integration.Load(t)
	token := env.Token(t)

	c, err := mgmt.NewClientWithResponses(env.Endpoint, mgmt.WithRequestEditorFn(func(_ context.Context, r *http.Request) error {
		r.Header.Set("Authorization", "Bearer "+token)
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.ListApplicationsWithResponse(context.Background(), &mgmt.ListApplicationsParams{})
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode() != http.StatusOK || res.JSON200 == nil {
		t.Fatalf("Logto %s: GET /api/applications answered %d", env.Version, res.StatusCode())
	}
	t.Logf("Logto %s: %d application(s) listed through the generated client", env.Version, len(*res.JSON200))
}

// A real Logto of the window is recognised by its own spec, whatever host it is reached at: the vendored document is
// what the instance serves.
func TestARealLogtoIsRecognisedByItsSpec(t *testing.T) {
	env := integration.Load(t)
	c := client.New(client.Config{Endpoint: env.Endpoint, TokenEndpoint: env.AdminEndpoint, ClientID: env.ClientID, ClientSecret: env.ClientSecret})
	spec, err := capability.Fetch(context.Background(), c)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Version != env.Version {
		t.Fatalf("Logto %s recognised as %q", env.Version, spec.Version)
	}
}
