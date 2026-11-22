package logtofake_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/logto/logtofake"
	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

// A collection of the fake answers like Logto's: a name in use is 422, an unknown id 404, on every write.
func TestACollectionRefusesANameTwiceAndAnUnknownID(t *testing.T) {
	srv := httptest.NewServer(logtofake.New("boot", "boot-secret").Handler())
	t.Cleanup(srv.Close)
	c := clientFor(t, srv.URL, "boot", "boot-secret")
	ctx := context.Background()

	body := mgmt.CreateOrganizationScopeJSONRequestBody{Name: "read:things"}
	first, err := c.CreateOrganizationScopeWithResponse(ctx, body)
	if err != nil || first.StatusCode() != http.StatusCreated || first.JSON201 == nil || first.JSON201.Id == "" {
		t.Fatalf("create: %v %+v", err, first)
	}
	again, err := c.CreateOrganizationScopeWithResponse(ctx, body)
	if err != nil || again.StatusCode() != http.StatusUnprocessableEntity {
		t.Fatalf("a name in use: %v, status %d, want 422", err, again.StatusCode())
	}
	patched, err := c.UpdateOrganizationScopeWithResponse(ctx, "nope", mgmt.UpdateOrganizationScopeJSONRequestBody{})
	if err != nil || patched.StatusCode() != http.StatusNotFound {
		t.Fatalf("patch of an unknown id: %v, status %d, want 404", err, patched.StatusCode())
	}
	deleted, err := c.DeleteOrganizationScopeWithResponse(ctx, "nope")
	if err != nil || deleted.StatusCode() != http.StatusNotFound {
		t.Fatalf("delete of an unknown id: %v, status %d, want 404", err, deleted.StatusCode())
	}
}
