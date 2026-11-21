package config_test

import (
	"strings"
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/config"
)

const smtpOK = `  connectors:
    smtp:
      connectorId: simple-mail-transfer-protocol
      smtp:
        host: smtp.example.test
        port: 587
        auth: {user: {value: u}, pass: {valueFrom: {env: P}}}
        fromEmail: a@example.test
        templates:
          items:
            - {usageType: Register, contentType: text/plain, subject: s, content: {value: c}}
            - {usageType: SignIn, contentType: text/plain, subject: s, content: {value: c}}
            - {usageType: ForgotPassword, contentType: text/plain, subject: s, content: {value: c}}
            - {usageType: Generic, contentType: text/plain, subject: s, content: {value: c}}
`

// Each rule: a document breaking it, and the path and message the violation must carry.
func TestEverySchemaRuleIsEnforced(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{
			"duplicate application name",
			"  applications:\n    a: {name: Same, type: SPA}\n    b: {name: Same, type: SPA}\n",
			`spec.applications.b: name "Same" is already declared by spec.applications.a`,
		},
		{
			"application type",
			"  applications:\n    a: {name: A, type: Desktop}\n",
			"spec.applications.a.type: must be one of",
		},
		{
			"roles on a SPA",
			"  applications:\n    a: {name: A, type: SPA, roles: [{name: R}]}\n",
			"spec.applications.a: roles and secret apply to a MachineToMachine application only",
		},
		{
			"dangling organization scope",
			"  organizationRoles:\n    owner: {name: owner, organizationScopes: [ghost]}\n",
			`spec.organizationRoles.owner.organizationScopes[0]: "ghost" is not declared in spec.organizationScopes`,
		},
		{
			"dangling resource",
			"  organizationRoles:\n    owner: {name: owner, resourceScopes: [{resource: nope, scope: x}]}\n",
			`spec.organizationRoles.owner.resourceScopes[0]: resource "nope" is not declared`,
		},
		{
			"dangling resource scope",
			"  resources:\n    app: {name: A, indicator: https://a.test/api, scopes: {read: {}}}\n" +
				"  organizationRoles:\n    owner: {name: owner, resourceScopes: [{resource: app, scope: write}]}\n",
			`scope "write" is not declared in spec.resources.app.scopes`,
		},
		{
			"scope with a space",
			"  organizationScopes:\n    \"read members\": {}\n",
			"spec.organizationScopes.read members: a scope name cannot be empty or contain whitespace",
		},
		{
			"equal audiences",
			"  resources:\n    a: {name: A, indicator: https://x.test/api}\n    b: {name: B, indicator: https://x.test/api}\n",
			`spec.resources.b: indicator "https://x.test/api" is already declared by spec.resources.a`,
		},
		{
			"SMTP port out of range",
			strings.Replace(smtpOK, "port: 587", "port: 0", 1),
			"spec.connectors.smtp.smtp.port: must be a number between 1 and 65535",
		},
		{
			"SMTP template missing",
			strings.Replace(smtpOK, "            - {usageType: ForgotPassword, contentType: text/plain, subject: s, content: {value: c}}\n", "", 1),
			"spec.connectors.smtp.smtp.templates: the ForgotPassword template is required",
		},
		{
			"SMTP auth empty",
			strings.Replace(smtpOK, "user: {value: u}", "user: {}", 1),
			"spec.connectors.smtp.smtp.auth.user: is required",
		},
		{
			"secret with two sources",
			strings.Replace(smtpOK, "pass: {valueFrom: {env: P}}", "pass: {valueFrom: {env: P, file: /p}}", 1),
			"spec.connectors.smtp.smtp.auth.pass.valueFrom: must name exactly one of env, file, secretKeyRef",
		},
		{
			"duplicate user",
			"  users:\n    a: {primaryEmail: Admin@example.test}\n    b: {primaryEmail: admin@example.test}\n",
			`spec.users.b: primaryEmail "admin@example.test" is already declared by spec.users.a`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg, err := config.Parse([]byte(minimal + c.body))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			err = config.Validate(cfg)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v\nwant an error containing %q", err, c.want)
			}
		})
	}
}

func TestAPortWrittenAsAStringIsRefusedAtDecode(t *testing.T) {
	_, err := config.Parse([]byte(minimal + strings.Replace(smtpOK, "port: 587", `port: "587"`, 1)))
	if err == nil {
		t.Fatal("want an error: Logto accepts a string port and the connector then fails at sign-up")
	}
}

func TestAllViolationsAreReportedAtOnce(t *testing.T) {
	cfg, err := config.Parse([]byte(minimal + "  applications:\n    a: {name: A, type: Desktop}\n  users:\n    u: {primaryEmail: nope}\n"))
	if err != nil {
		t.Fatal(err)
	}
	err = config.Validate(cfg)
	if err == nil || !strings.Contains(err.Error(), "spec.applications.a.type") || !strings.Contains(err.Error(), "spec.users.u.primaryEmail") {
		t.Fatalf("err = %v", err)
	}
}
