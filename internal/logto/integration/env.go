//go:build integration

// Package integration gives integration tests the credentials of the throwaway Logto started by `task it:up`
// (test/integration/compose.yaml), written by `task it:bootstrap` to build/it.env. Built only with the `integration`
// tag: `task test` never compiles it.
package integration

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ManagementResource is the API resource of the default tenant's Management API.
const ManagementResource = "https://default.logto.app/api"

// Env is the throwaway Logto of the current run.
type Env struct {
	Endpoint      string // tenant endpoint: the Management API answers here
	AdminEndpoint string // admin endpoint: m-default gets its token here
	ClientID      string
	ClientSecret  string
	Version       string
}

// Load reads build/it.env, or skips the test with the command to run when the harness is not up.
func Load(t *testing.T) Env {
	t.Helper()
	path := filepath.Join(repoRoot(t), "build", "it.env")
	f, err := os.Open(path)
	if err != nil {
		// `task it` sets LOGTO_IT_REQUIRED: there, a missing harness is a failure, never a silent skip.
		if os.Getenv("LOGTO_IT_REQUIRED") != "" {
			t.Fatalf("no integration Logto (%s missing) although LOGTO_IT_REQUIRED is set", path)
		}
		t.Skipf("no integration Logto (%s missing): run `task it:up it:bootstrap`, or `task it`", path)
	}
	defer func() { _ = f.Close() }()
	values := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if k, v, ok := strings.Cut(sc.Text(), "="); ok {
			values[k] = v
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return Env{
		Endpoint:      values["LOGTO_IT_ENDPOINT"],
		AdminEndpoint: values["LOGTO_IT_ADMIN_ENDPOINT"],
		ClientID:      values["LOGTO_IT_CLIENT_ID"],
		ClientSecret:  values["LOGTO_IT_CLIENT_SECRET"],
		Version:       values["LOGTO_IT_VERSION"],
	}
}

// Token obtains a Management API token through the m-default route: client_credentials at the admin endpoint.
func (e Env) Token(t *testing.T) string {
	t.Helper()
	form := url.Values{"grant_type": {"client_credentials"}, "resource": {ManagementResource}, "scope": {"all"}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.AdminEndpoint+"/oidc/token", strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(e.ClientID, e.ClientSecret)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var body struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil || body.AccessToken == "" {
		t.Fatalf("no Management API token from %s (status %d, error %q)", e.AdminEndpoint, res.StatusCode, body.Error)
	}
	return body.AccessToken
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}
