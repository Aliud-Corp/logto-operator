//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/logto/client"
	"github.com/Aliud-Corp/logto-operator/internal/logto/integration"
	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

const smtpPassword = "acceptance-smtp-password"

type cliResult struct {
	code           int
	stdout, stderr string
}

// The M1 definition of done, on an empty real Logto: the built logtoctl applies examples/reference.yaml, every
// declared object is there (checked through the API), a second apply writes nothing, check exits 0 then 2 after a
// hand edit, render shows no secret, and the published M2M secret obtains a Management API token.
func TestM1AcceptanceWithTheReferenceDocument(t *testing.T) {
	env := integration.Load(t)
	bin := filepath.Join(t.TempDir(), "logtoctl")
	build := exec.CommandContext(context.Background(), "go", "build", "-o", bin, "./cmd/logtoctl")
	build.Dir = "../.."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build logtoctl: %v\n%s", err, out)
	}
	doc := "../../examples/reference.yaml"
	secretsFile := filepath.Join(t.TempDir(), "secrets.json")
	cmdEnv := append(os.Environ(), "LOGTO_CLIENT_ID="+env.ClientID, "LOGTO_CLIENT_SECRET="+env.ClientSecret, "EXAMPLE_SMTP_PASSWORD="+smtpPassword)
	run := func(args ...string) cliResult {
		cmd := exec.CommandContext(context.Background(), bin, args...)
		cmd.Env = cmdEnv
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		err := cmd.Run()
		code := 0
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return cliResult{code, out.String(), errb.String()}
	}
	noLeak := func(r cliResult, values ...string) {
		t.Helper()
		for _, v := range values {
			if v != "" && strings.Contains(r.stdout+r.stderr, v) {
				t.Fatalf("a secret leaked:\n--- stdout\n%s\n--- stderr\n%s", r.stdout, r.stderr)
			}
		}
	}

	a := run("apply", "-f", doc, "--secrets-out", secretsFile)
	if a.code != 0 {
		t.Fatalf("Logto %s: apply exited %d\n%s%s", env.Version, a.code, a.stdout, a.stderr)
	}
	var published map[string]string
	b, _ := os.ReadFile(secretsFile)
	if err := json.Unmarshal(b, &published); err != nil || published["applications.backend.secret"] == "" {
		t.Fatalf("secrets file: %s (%v)", b, err)
	}
	m2mSecret := published["applications.backend.secret"]
	noLeak(a, env.ClientSecret, smtpPassword, m2mSecret)

	c := client.New(client.Config{Endpoint: env.Endpoint, TokenEndpoint: env.AdminEndpoint, ClientID: env.ClientID, ClientSecret: env.ClientSecret})
	assertObjects(t, c)

	ids := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(a.stdout), "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			ids[k] = v
		}
	}
	m2m := client.New(client.Config{Endpoint: env.Endpoint, ClientID: ids["applications.backend.id"], ClientSecret: m2mSecret})
	if _, err := m2m.Token(context.Background()); err != nil {
		t.Fatalf("the published M2M secret gets no Management API token: %v", err)
	}

	again := run("apply", "-f", doc, "--secrets-out", secretsFile)
	if again.code != 0 || !strings.Contains(again.stderr, "Plan: 0 to create, 0 to update, 0 to delete") {
		t.Fatalf("second apply (%d) wrote something:\n%s", again.code, again.stderr)
	}
	if ch := run("check", "-f", doc); ch.code != 0 {
		t.Fatalf("check after apply exited %d:\n%s", ch.code, ch.stdout)
	}

	req, err := mgmt.NewUpdateApplicationRequestWithBody(c.Endpoint(), ids["applications.app.id"], "application/json",
		strings.NewReader(`{"oidcClientMetadata":{"redirectUris":["https://elsewhere.example/callback"],"postLogoutRedirectUris":["https://app.example.test"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Do(context.Background(), req, nil); err != nil {
		t.Fatal(err)
	}
	ch := run("check", "-f", doc)
	if ch.code != 2 || !strings.Contains(ch.stdout, "oidcClientMetadata.redirectUris") {
		t.Fatalf("check after a hand edit exited %d:\n%s", ch.code, ch.stdout)
	}
	noLeak(ch, env.ClientSecret, smtpPassword, m2mSecret)
	if r := run("render", "-f", doc); r.code != 0 {
		t.Fatalf("render exited %d", r.code)
	} else {
		noLeak(r, env.ClientSecret, smtpPassword, m2mSecret)
	}
	if fix := run("apply", "-f", doc, "--secrets-out", secretsFile); fix.code != 0 {
		t.Fatalf("repair apply exited %d:\n%s", fix.code, fix.stderr)
	}
	if ch := run("check", "-f", doc); ch.code != 0 {
		t.Fatalf("check after the repair exited %d:\n%s", ch.code, ch.stdout)
	}
}

// assertObjects checks, through the Management API, every object the reference document declares.
func assertObjects(t *testing.T, c *client.Client) {
	t.Helper()
	ctx := context.Background()
	get := func(req *http.Request, err error, out any) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		if err := c.Do(ctx, req, out); err != nil {
			t.Fatal(err)
		}
	}
	page, size := 1, 100

	var resources []struct{ ID, Name, Indicator string }
	req, err := mgmt.NewListResourcesRequest(c.Endpoint(), &mgmt.ListResourcesParams{Page: &page, PageSize: &size})
	get(req, err, &resources)
	byIndicator := map[string]string{}
	for _, r := range resources {
		byIndicator[r.Indicator] = r.ID
	}
	for _, ind := range []string{"https://app.example.test/api", "https://admin.example.test/api"} {
		if byIndicator[ind] == "" {
			t.Errorf("resource %s missing", ind)
		}
	}
	var scopes []struct{ Name string }
	req, err = mgmt.NewListResourceScopesRequest(c.Endpoint(), byIndicator["https://app.example.test/api"], &mgmt.ListResourceScopesParams{Page: &page, PageSize: &size})
	get(req, err, &scopes)
	if names := names(scopes); names != "event:read,event:write" {
		t.Errorf("app scopes = %s", names)
	}

	var roles []struct {
		Name           string
		Scopes         []struct{ Name string }
		ResourceScopes []struct{ Name string } `json:"resourceScopes"`
	}
	req, err = mgmt.NewListOrganizationRolesRequest(c.Endpoint(), &mgmt.ListOrganizationRolesParams{Page: &page, PageSize: &size})
	get(req, err, &roles)
	found := map[string]string{}
	for _, r := range roles {
		found[r.Name] = names(r.Scopes) + "|" + names(r.ResourceScopes)
	}
	if found["organization_owner"] != "members:read,members:write|event:read,event:write" || found["organization_member"] != "members:read|event:read" {
		t.Errorf("organization roles = %v", found)
	}

	var global []struct{ ID, Name, Type string }
	req, err = mgmt.NewListRolesRequest(c.Endpoint(), &mgmt.ListRolesParams{Page: &page, PageSize: &size})
	get(req, err, &global)
	held := map[string]string{}
	for _, r := range global {
		var scopes []struct{ Name string }
		req, err = mgmt.NewListRoleScopesRequest(c.Endpoint(), r.ID, &mgmt.ListRoleScopesParams{Page: &page, PageSize: &size})
		get(req, err, &scopes)
		held[r.Name] = r.Type + "|" + names(scopes)
	}
	if held["platform_admin"] != "User|all" || held["event_sync"] != "MachineToMachine|event:read,event:write" {
		t.Errorf("global roles = %v", held)
	}

	var fields []struct{ Name, Type string }
	req, err = mgmt.NewListCustomProfileFieldsRequest(c.Endpoint())
	get(req, err, &fields)
	if !contains(fields, func(f struct{ Name, Type string }) bool { return f.Name == "fullname" && f.Type == "Fullname" }) {
		t.Errorf("profile field fullname missing: %v", fields)
	}

	var connector struct {
		Config struct{ Host string } `json:"config"`
	}
	req, err = mgmt.NewGetConnectorRequest(c.Endpoint(), "example-smtp")
	get(req, err, &connector)
	if connector.Config.Host != "smtp.example.test" {
		t.Errorf("connector host = %q", connector.Config.Host)
	}

	var sie struct {
		Color struct {
			PrimaryColor string `json:"primaryColor"`
		} `json:"color"`
		LanguageInfo struct {
			FallbackLanguage string `json:"fallbackLanguage"`
		} `json:"languageInfo"`
		CustomCSS string `json:"customCss"`
		Branding  struct {
			LogoURL string `json:"logoUrl"`
		} `json:"branding"`
		UnknownSessionRedirectURL string `json:"unknownSessionRedirectUrl"`
	}
	req, err = mgmt.NewGetSignInExpRequest(c.Endpoint())
	get(req, err, &sie)
	if sie.Color.PrimaryColor != "#101418" || sie.LanguageInfo.FallbackLanguage != "en" || !strings.Contains(sie.CustomCSS, "border-radius") ||
		!strings.HasPrefix(sie.Branding.LogoURL, "data:image/svg+xml;base64,") || sie.UnknownSessionRedirectURL != "https://app.example.test" {
		t.Errorf("sign-in experience = %+v", sie)
	}

	var apps []struct{ ID, Name, Type string }
	req, err = mgmt.NewListApplicationsRequest(c.Endpoint(), &mgmt.ListApplicationsParams{Page: &page, PageSize: &size})
	get(req, err, &apps)
	types, ids := map[string]string{}, map[string]string{}
	for _, a := range apps {
		types[a.Name], ids[a.Name] = a.Type, a.ID
	}
	if types["Example App"] != "SPA" || types["Example Backend"] != "MachineToMachine" {
		t.Errorf("applications = %v", types)
	}
	var granted []struct{ Name string }
	req, err = mgmt.NewListApplicationRolesRequest(c.Endpoint(), ids["Example Backend"], &mgmt.ListApplicationRolesParams{Page: &page, PageSize: &size})
	get(req, err, &granted)
	if got := names(granted); got != "Logto Management API access,event_sync" {
		t.Errorf("roles of Example Backend = %s", got)
	}

	var users []struct {
		ID           string `json:"id"`
		PrimaryEmail string `json:"primaryEmail"`
	}
	search := map[string]string{"search.primaryEmail": "admin@example.test"}
	req, err = mgmt.NewListUsersRequest(c.Endpoint(), &mgmt.ListUsersParams{Page: &page, PageSize: &size, SearchParams: &search})
	get(req, err, &users)
	if len(users) != 1 {
		t.Fatalf("users admin@example.test = %v", users)
	}
	var userRoles []struct{ Name string }
	req, err = mgmt.NewListUserRolesRequest(c.Endpoint(), users[0].ID, &mgmt.ListUserRolesParams{Page: &page, PageSize: &size})
	get(req, err, &userRoles)
	if got := names(userRoles); got != "platform_admin" {
		t.Errorf("roles of admin@example.test = %s", got)
	}
}

func names[T interface{ ~struct{ Name string } }](l []T) string {
	var out []string
	for _, x := range l {
		out = append(out, struct{ Name string }(x).Name)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func contains[T any](l []T, ok func(T) bool) bool {
	for _, x := range l {
		if ok(x) {
			return true
		}
	}
	return false
}
