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
			"spec.applications.a.type: value is not one of the allowed values",
		},
		{
			"consent scopes on a first-party application",
			"  applications:\n    a: {name: A, type: Native, consentScopes: {user: [email]}}\n",
			"spec.applications.a.consentScopes: applies to a third-party application only (set isThirdParty: true)",
		},
		{
			"consent scopes on an application declared first-party",
			"  applications:\n    a: {name: A, type: Native, isThirdParty: false, consentScopes: {user: [email]}}\n",
			"spec.applications.a.consentScopes: applies to a third-party application only (set isThirdParty: true)",
		},
		{
			"a third-party machine-to-machine application",
			"  applications:\n    a: {name: A, type: MachineToMachine, isThirdParty: true}\n",
			"spec.applications.a.isThirdParty: applies to a Native, SPA or Traditional application",
		},
		{
			"a consent scope of an undeclared resource",
			"  applications:\n    a: {name: A, type: Traditional, isThirdParty: true, consentScopes: {resources: [{resource: app, scope: read}]}}\n",
			`spec.applications.a.consentScopes.resources[0]: resource "app" is not declared in spec.resources`,
		},
		{
			"a user consent scope listed twice",
			"  applications:\n    a: {name: A, type: Traditional, isThirdParty: true, consentScopes: {user: [email, email]}}\n",
			`spec.applications.a.consentScopes.user[1]: user scope "email" is already listed`,
		},
		{
			"a user consent scope Logto does not know",
			"  applications:\n    a: {name: A, type: Traditional, isThirdParty: true, consentScopes: {user: [passport]}}\n",
			"spec.applications.a.consentScopes.user.0: value is not one of the allowed values",
		},
		{
			"roles on a SPA",
			"  applications:\n    a: {name: A, type: SPA, roles: [{name: R}]}\n",
			"spec.applications.a: roles and secret apply to a MachineToMachine application only",
		},
		{
			"an output key naming no output",
			"  applications:\n    web: {name: Web, type: SPA}\n    backend: {name: Backend, type: MachineToMachine, secret: {publish: true}}\n  outputs:\n    configMap: {name: ids, keys: {APP_ID: applications.nope.id}}\n",
			`spec.outputs.configMap.keys.APP_ID: "applications.nope.id" is no output of this document`,
		},
		{
			"a secret published in the ConfigMap",
			"  applications:\n    web: {name: Web, type: SPA}\n    backend: {name: Backend, type: MachineToMachine, secret: {publish: true}}\n  outputs:\n    configMap: {name: ids, keys: {SECRET: applications.backend.secret}}\n",
			`spec.outputs.configMap.keys.SECRET: "applications.backend.secret" is a secret: publish it in spec.outputs.secret`,
		},
		{
			"an identifier published in the Secret",
			"  applications:\n    web: {name: Web, type: SPA}\n    backend: {name: Backend, type: MachineToMachine, secret: {publish: true}}\n  outputs:\n    secret: {name: s, keys: {APP_ID: applications.web.id}}\n",
			`spec.outputs.secret.keys.APP_ID: "applications.web.id" is an identifier: publish it in spec.outputs.configMap`,
		},
		{
			"an output key Kubernetes refuses",
			"  applications:\n    web: {name: Web, type: SPA}\n    backend: {name: Backend, type: MachineToMachine, secret: {publish: true}}\n  outputs:\n    configMap: {name: ids, keys: {\"APP ID\": applications.web.id}}\n",
			`spec.outputs.configMap.keys.APP ID: not a valid ConfigMap or Secret key`,
		},
		{
			"a dot output key",
			"  applications:\n    web: {name: Web, type: SPA}\n    backend: {name: Backend, type: MachineToMachine, secret: {publish: true}}\n  outputs:\n    configMap: {name: ids, keys: {\"..\": applications.web.id}}\n",
			`spec.outputs.configMap.keys...: not a valid ConfigMap or Secret key`,
		},
		{
			"an output name Kubernetes refuses, without keys",
			"  applications:\n    \"my app\": {name: Web, type: SPA}\n  outputs:\n    configMap: {name: ids}\n",
			`spec.outputs.configMap: the output "applications.my app.id" is no valid ConfigMap key: name it with keys`,
		},
		{
			"one key in both targets",
			"  applications:\n    web: {name: Web, type: SPA}\n    backend: {name: Backend, type: MachineToMachine, secret: {publish: true}}\n  outputs:\n    configMap: {name: ids, keys: {X: applications.web.id}}\n    secret: {name: s, keys: {X: applications.backend.secret}}\n",
			`spec.outputs.secret.keys.X: also published by spec.outputs.configMap; with envFrom one would hide the other`,
		},
		{
			"a ConfigMap key hiding a Secret output published without keys",
			"  applications:\n    web: {name: Web, type: SPA}\n    backend: {name: Backend, type: MachineToMachine, secret: {publish: true}}\n  outputs:\n    configMap: {name: ids, keys: {applications.backend.secret: applications.backend.id}}\n    secret: {name: s}\n",
			`spec.outputs.secret: the output "applications.backend.secret" is also published by spec.outputs.configMap`,
		},
		{
			"a resync of a second",
			"  resyncInterval: 1s\n",
			`spec.resyncInterval: at least 30s`,
		},
		{
			"duplicate role name",
			"  roles:\n    a: {name: Same, description: d, type: User}\n    b: {name: Same, description: d, type: User}\n",
			`spec.roles.b: name "Same" is already declared by spec.roles.a`,
		},
		{
			"role type",
			"  roles:\n    a: {name: A, description: d, type: Admin}\n",
			"spec.roles.a.type: value is not one of the allowed values",
		},
		{
			"role without a description",
			"  roles:\n    a: {name: A, type: User}\n",
			"spec.roles.a.description",
		},
		{
			"default machine role",
			"  roles:\n    a: {name: A, description: d, type: MachineToMachine, isDefault: true}\n",
			"spec.roles.a.isDefault: applies to a User role only",
		},
		{
			"dangling role scope",
			"  resources:\n    app: {name: A, indicator: https://a.test/api, scopes: {read: {}}}\n" +
				"  roles:\n    a: {name: A, description: d, type: User, scopes: [{resource: app, scope: write}]}\n",
			`spec.roles.a.scopes[0]: scope "write" is not declared in spec.resources.app.scopes`,
		},
		{
			"duplicate role scope",
			"  resources:\n    app: {name: A, indicator: https://a.test/api, scopes: {read: {}}}\n" +
				"  roles:\n    a: {name: A, description: d, type: User, scopes: [{resource: app, scope: read}, {resource: app, scope: read}]}\n",
			`spec.roles.a.scopes[1]: scope "read" of resource "app" is already listed`,
		},
		{
			"duplicate organization role resource scope",
			"  resources:\n    app: {name: A, indicator: https://a.test/api, scopes: {read: {}}}\n" +
				"  organizationRoles:\n    o: {name: o, resourceScopes: [{resource: app, scope: read}, {resource: app, scope: read}]}\n",
			`spec.organizationRoles.o.resourceScopes[1]: scope "read" of resource "app" is already listed`,
		},
		{
			"a User role granted to an application",
			"  roles:\n    viewer: {name: Viewer, description: d, type: User}\n" +
				"  applications:\n    a: {name: A, type: MachineToMachine, roles: [{name: Viewer}]}\n",
			`spec.applications.a.roles[0]: "Viewer" is a User role (spec.roles.viewer); an application takes MachineToMachine roles`,
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
			"spec.connectors.smtp.smtp.port: number must be at least 1",
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
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := check(minimal + c.body)
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
	// The schema rules, all at once, before anything else...
	err := check(minimal + "  applications:\n    a: {name: A, type: Desktop}\n  resources:\n    r: {indicator: https://r.test/api}\n")
	if err == nil || !strings.Contains(err.Error(), "spec.applications.a.type") || !strings.Contains(err.Error(), "spec.resources.r.name") {
		t.Fatalf("err = %v", err)
	}
	// ...then the rules across fields, all at once.
	err = check(minimal + "  applications:\n    a: {name: Same, type: SPA}\n    b: {name: Same, type: SPA}\n  roles:\n    a: {name: Same, description: d, type: User}\n    b: {name: Same, description: d, type: User}\n")
	if err == nil || !strings.Contains(err.Error(), "spec.applications.b") || !strings.Contains(err.Error(), "spec.roles.b") {
		t.Fatalf("err = %v", err)
	}
}

// The rules of the generated JSON Schema (required, pattern, enum, minimum: what the CRD refuses) are checked by the
// CLI too, before any call, together with the other rules.
func TestTheJSONSchemaRulesAreEnforced(t *testing.T) {
	for name, c := range map[string]struct{ body, want string }{
		"resource without a name": {
			"  resources:\n    app: {indicator: https://a.test/api}\n", "spec.resources.app.name",
		},
		"an endpoint that is no URL": {
			"", "spec.endpoint",
		},
		"an unknown file conversion": {
			"  signInExperience:\n    branding: {logoUrl: {fromFile: logo.png, as: datauri}}\n", "spec.signInExperience.branding.logoUrl.as",
		},
		"a token lifetime of zero": {
			"  resources:\n    app: {name: A, indicator: https://a.test/api, accessTokenTtl: 0}\n", "spec.resources.app.accessTokenTtl",
		},
		"a colour that is no colour": {
			"  signInExperience:\n    color: {primaryColor: blue}\n", "spec.signInExperience.color.primaryColor",
		},
	} {
		t.Run(name, func(t *testing.T) {
			doc := minimal + c.body
			if name == "an endpoint that is no URL" {
				doc = strings.Replace(minimal, "    endpoint: http://localhost:3001", "    endpoint: localhost:3001", 1)
			}
			if err := check(doc); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("got %v\nwant an error naming %s", err, c.want)
			}
		})
	}
}

// check is what a command does before any call: parse (with the schema rules), then the rules across fields.
func check(doc string) error {
	cfg, err := config.Parse([]byte(doc))
	if err != nil {
		return err
	}
	return config.Validate(cfg)
}
