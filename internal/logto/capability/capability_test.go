package capability_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	logtospecs "github.com/Aliud-Corp/logto-operator/api/logto"
	"github.com/Aliud-Corp/logto-operator/internal/logto/capability"
	"github.com/Aliud-Corp/logto-operator/internal/logto/client"
	"github.com/Aliud-Corp/logto-operator/internal/logto/logtofake"
)

func target(t *testing.T, version string) *capability.Spec {
	t.Helper()
	raw, err := logtospecs.Spec(version)
	if err != nil {
		t.Fatal(err)
	}
	fake := logtofake.New("boot", "boot-secret")
	fake.ServeSpec(raw)
	srv := httptest.NewServer(fake.Handler())
	t.Cleanup(srv.Close)
	s, err := capability.Fetch(context.Background(), client.New(client.Config{Endpoint: srv.URL, ClientID: "boot", ClientSecret: "boot-secret"}))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestADeclaredFieldTheTargetLacksIsRefused(t *testing.T) {
	need := capability.Need{OperationID: "UpdateSignInExp", Fields: []string{"crossOriginOpenerPolicy"}, Where: "spec.signInExperience"}
	if err := target(t, "1.46.0").Check(need); err != nil {
		t.Fatalf("1.46.0 has the field: %v", err)
	}
	err := target(t, "1.44.0").Check(need)
	if err == nil || !strings.Contains(err.Error(), "spec.signInExperience.crossOriginOpenerPolicy: this Logto does not accept") {
		t.Fatalf("err = %v", err)
	}
}

func TestFieldsListsWhatWillBeSent(t *testing.T) {
	type wire struct {
		Color *struct {
			Primary string `json:"primaryColor"`
		} `json:"color,omitempty"`
		URL string `json:"unknownSessionRedirectUrl,omitempty"`
	}
	got, err := capability.Fields(wire{URL: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "unknownSessionRedirectUrl" {
		t.Fatalf("fields = %v (an omitted field must not be checked)", got)
	}
}
