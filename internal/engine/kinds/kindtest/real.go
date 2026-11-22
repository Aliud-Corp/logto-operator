//go:build integration

package kindtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"

	"github.com/Aliud-Corp/logto-operator/api/v1alpha1"
	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"

	"github.com/Aliud-Corp/logto-operator/internal/logto/client"
	"github.com/Aliud-Corp/logto-operator/internal/logto/integration"
)

// Real returns a client on the throwaway Logto of the integration harness (m-default route), or skips.
func Real(t *testing.T) *client.Client {
	t.Helper()
	env := integration.Load(t)
	return client.New(client.Config{
		Endpoint: env.Endpoint, TokenEndpoint: env.AdminEndpoint,
		ClientID: env.ClientID, ClientSecret: env.ClientSecret,
	})
}

// Unique is a suffix that keeps the objects of one test apart from the others' in the shared throwaway Logto.
func Unique(t *testing.T) string {
	t.Helper()
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

// KeepAllBut adds to spec.prune.keep the name of every resource, organization scope, organization role and custom
// profile field of the shared Logto that does not contain suffix, so a prune in one test only touches its own
// objects. Integration packages run one at a time (`go test -p 1`), so no object appears meanwhile.
func KeepAllBut(t *testing.T, c *client.Client, spec *v1alpha1.LogtoConfigSpec, suffix string) *v1alpha1.LogtoConfigSpec {
	t.Helper()
	type named struct {
		Name string `json:"name"`
	}
	lists := []func(page, size int) (*http.Request, error){
		func(page, size int) (*http.Request, error) {
			return mgmt.NewListResourcesRequest(c.Endpoint(), &mgmt.ListResourcesParams{Page: &page, PageSize: &size})
		},
		func(page, size int) (*http.Request, error) {
			return mgmt.NewListOrganizationScopesRequest(c.Endpoint(), &mgmt.ListOrganizationScopesParams{Page: &page, PageSize: &size})
		},
		func(page, size int) (*http.Request, error) {
			return mgmt.NewListOrganizationRolesRequest(c.Endpoint(), &mgmt.ListOrganizationRolesParams{Page: &page, PageSize: &size})
		},
		func(page, size int) (*http.Request, error) {
			return mgmt.NewListRolesRequest(c.Endpoint(), &mgmt.ListRolesParams{Page: &page, PageSize: &size})
		},
	}
	for _, build := range lists {
		items, err := client.List[named](context.Background(), c, build)
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range items {
			if !strings.Contains(it.Name, suffix) {
				spec.Prune.Keep = append(spec.Prune.Keep, it.Name)
			}
		}
	}
	req, err := mgmt.NewListCustomProfileFieldsRequest(c.Endpoint())
	if err != nil {
		t.Fatal(err)
	}
	var fields []named
	if err := c.Do(context.Background(), req, &fields); err != nil {
		t.Fatal(err)
	}
	for _, f := range fields {
		if !strings.Contains(f.Name, suffix) {
			spec.Prune.Keep = append(spec.Prune.Keep, f.Name)
		}
	}
	return spec
}
