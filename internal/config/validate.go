package config

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/Aliud-Corp/logto-operator/api/v1alpha1"
)

// RequiredSMTPTemplates are the usage types the SMTP connector refuses to be created without (measured on Logto
// 1.44.0 and 1.46.0: "Template with UsageType (Generic) should be provided!").
var RequiredSMTPTemplates = []string{"Register", "SignIn", "ForgotPassword", "Generic"}

// MinResync is the shortest spec.resyncInterval.
const MinResync = 30 * time.Second

// Validate applies the schema rules of a loaded document and returns every violation at once, each with its path.
func Validate(cfg *v1alpha1.LogtoConfig) error {
	v := &validator{}
	s := &cfg.Spec
	if s.Endpoint == "" {
		v.add("spec.endpoint", "is required")
	}
	v.stringSource("spec.credentials.clientId", s.Credentials.ClientID)
	v.valueFrom("spec.credentials.clientSecret.valueFrom", s.Credentials.ClientSecret.ValueFrom)

	v.connectors(s)
	uniqueBy(v, "spec.customProfileFields", s.CustomProfileFields, "name", func(f v1alpha1.CustomProfileField) string { return f.Name })
	v.resources(s)
	for _, k := range sortedKeys(s.OrganizationScopes) {
		v.scopeName("spec.organizationScopes."+k, k)
	}
	v.organizationRoles(s)
	v.roles(s)
	v.applications(s)
	v.outputs(s)
	if r := s.ResyncInterval; r != nil && r.Duration < MinResync {
		v.add("spec.resyncInterval", "at least %s: every resync plans against Logto", MinResync)
	}
	return errors.Join(v.errs...)
}

type validator struct{ errs []error }

func (v *validator) add(path, format string, args ...any) {
	v.errs = append(v.errs, fmt.Errorf("%s: %s", path, fmt.Sprintf(format, args...)))
}

func (v *validator) stringSource(path string, s v1alpha1.StringSource) {
	switch {
	case s.Value != "" && s.ValueFrom != nil:
		v.add(path, "value and valueFrom are exclusive")
	case s.Value == "" && s.ValueFrom == nil:
		v.add(path, "is required (value or valueFrom)")
	case s.ValueFrom != nil:
		v.valueFrom(path+".valueFrom", *s.ValueFrom)
	}
}

func (v *validator) valueFrom(path string, f v1alpha1.ValueFrom) {
	n := 0
	if f.Env != "" {
		n++
	}
	if f.File != "" {
		n++
	}
	if f.SecretKeyRef != nil {
		n++
	}
	if n != 1 {
		v.add(path, "must name exactly one of env, file, secretKeyRef")
	}
}

func (v *validator) scopeName(path, name string) {
	if name == "" || strings.ContainsFunc(name, unicode.IsSpace) {
		v.add(path, "a scope name cannot be empty or contain whitespace")
	}
}

func uniqueBy[T any](v *validator, path string, m map[string]T, what string, key func(T) string) {
	seen := map[string]string{}
	for _, k := range sortedKeys(m) {
		name := key(m[k])
		if name == "" {
			continue
		}
		if other, dup := seen[name]; dup {
			v.add(path+"."+k, "%s %q is already declared by %s", what, name, path+"."+other)
			continue
		}
		seen[name] = k
	}
}

func (v *validator) connectors(s *v1alpha1.LogtoConfigSpec) {
	uniqueBy(v, "spec.connectors", s.Connectors, "id", func(c v1alpha1.Connector) string { return c.ID })
	for _, k := range sortedKeys(s.Connectors) {
		c, path := s.Connectors[k], "spec.connectors."+k
		if c.ConnectorID == "" {
			v.add(path+".connectorId", "is required")
		}
		if c.SMTP == nil {
			v.add(path, "only the SMTP connector (smtp) is supported")
			continue
		}
		smtp := c.SMTP
		if smtp.Port < 1 || smtp.Port > 65535 {
			v.add(path+".smtp.port", "must be a number between 1 and 65535")
		}
		v.stringSource(path+".smtp.auth.user", smtp.Auth.User)
		v.valueFrom(path+".smtp.auth.pass.valueFrom", smtp.Auth.Pass.ValueFrom)
		have := map[string]bool{}
		for _, t := range smtp.Templates.Items {
			have[t.UsageType] = true
		}
		for _, req := range RequiredSMTPTemplates {
			if !have[req] {
				v.add(path+".smtp.templates", "the %s template is required by the SMTP connector", req)
			}
		}
	}
}

func (v *validator) resources(s *v1alpha1.LogtoConfigSpec) {
	uniqueBy(v, "spec.resources", s.Resources, "indicator", func(r v1alpha1.Resource) string { return r.Indicator })
	uniqueBy(v, "spec.resources", s.Resources, "name", func(r v1alpha1.Resource) string { return r.Name })
	for _, k := range sortedKeys(s.Resources) {
		r := s.Resources[k]
		for _, sc := range sortedKeys(r.Scopes) {
			v.scopeName("spec.resources."+k+".scopes."+sc, sc)
		}
	}
}

func (v *validator) organizationRoles(s *v1alpha1.LogtoConfigSpec) {
	uniqueBy(v, "spec.organizationRoles", s.OrganizationRoles, "name", func(r v1alpha1.OrganizationRole) string { return r.Name })
	for _, k := range sortedKeys(s.OrganizationRoles) {
		r, path := s.OrganizationRoles[k], "spec.organizationRoles."+k
		for i, sc := range r.OrganizationScopes {
			if _, ok := s.OrganizationScopes[sc]; !ok {
				v.add(fmt.Sprintf("%s.organizationScopes[%d]", path, i), "%q is not declared in spec.organizationScopes", sc)
			}
		}
		v.resourceScopes(s, path+".resourceScopes", r.ResourceScopes)
	}
}

// resourceScopes checks that each reference names a declared resource and one of its declared scopes, once.
func (v *validator) resourceScopes(s *v1alpha1.LogtoConfigSpec, path string, refs []v1alpha1.ResourceScopeRef) {
	seen := map[v1alpha1.ResourceScopeRef]bool{}
	for i, rs := range refs {
		p := fmt.Sprintf("%s[%d]", path, i)
		if seen[rs] {
			v.add(p, "scope %q of resource %q is already listed", rs.Scope, rs.Resource)
			continue
		}
		seen[rs] = true
		res, ok := s.Resources[rs.Resource]
		if !ok {
			v.add(p, "resource %q is not declared in spec.resources", rs.Resource)
			continue
		}
		if _, ok := res.Scopes[rs.Scope]; !ok {
			v.add(p, "scope %q is not declared in spec.resources.%s.scopes", rs.Scope, rs.Resource)
		}
	}
}

func (v *validator) roles(s *v1alpha1.LogtoConfigSpec) {
	uniqueBy(v, "spec.roles", s.Roles, "name", func(r v1alpha1.Role) string { return r.Name })
	for _, k := range sortedKeys(s.Roles) {
		r, path := s.Roles[k], "spec.roles."+k
		if r.IsDefault != nil && *r.IsDefault && r.Type != "User" {
			v.add(path+".isDefault", "applies to a User role only")
		}
		if r.Scopes != nil {
			v.resourceScopes(s, path+".scopes", *r.Scopes)
		}
	}
}

// grantable checks that the declared roles a grant names are of the type the grantee takes; a role the document
// does not declare is Logto's to know, and checked by the plan.
func (v *validator) grantable(s *v1alpha1.LogtoConfigSpec, path string, refs []v1alpha1.RoleRef, typ, grantee string) {
	for i, ref := range refs {
		for _, k := range sortedKeys(s.Roles) {
			if r := s.Roles[k]; r.Name == ref.Name && r.Type != typ {
				v.add(fmt.Sprintf("%s.roles[%d]", path, i), "%q is a %s role (spec.roles.%s); %s takes %s roles", ref.Name, r.Type, k, grantee, typ)
			}
		}
	}
}

func (v *validator) applications(s *v1alpha1.LogtoConfigSpec) {
	uniqueBy(v, "spec.applications", s.Applications, "name", func(a v1alpha1.Application) string { return a.Name })
	for _, k := range sortedKeys(s.Applications) {
		a, path := s.Applications[k], "spec.applications."+k
		if a.Name == "" {
			v.add(path+".name", "is required")
		}
		switch a.Type {
		case "Native", "SPA", "Traditional", "MachineToMachine":
		default:
			v.add(path+".type", "must be one of Native, SPA, Traditional, MachineToMachine")
		}
		if a.Type != "MachineToMachine" && (len(a.Roles) > 0 || a.Secret != nil) {
			v.add(path, "roles and secret apply to a MachineToMachine application only")
		}
		v.grantable(s, path, a.Roles, "MachineToMachine", "an application")
	}
}

// Outputs are the names of what a document publishes: identifiers and secrets. The kinds publish exactly these
// (a test of internal/engine/kinds checks it on examples/reference.yaml).
func Outputs(s *v1alpha1.LogtoConfigSpec) (ids, secrets map[string]bool) {
	ids, secrets = map[string]bool{}, map[string]bool{}
	for k, a := range s.Applications {
		ids["applications."+k+".id"] = true
		if a.Secret != nil && a.Secret.Publish {
			secrets["applications."+k+".secret"] = true
		}
	}
	for k := range s.Resources {
		ids["resources."+k+".indicator"] = true
	}
	return ids, secrets
}

// outputKey is what Kubernetes accepts as a ConfigMap or Secret key.
var outputKey = regexp.MustCompile(`^[-._a-zA-Z0-9]+$`)

// validKey reports whether Kubernetes accepts a ConfigMap or Secret key.
func validKey(key string) bool {
	return len(key) <= 253 && outputKey.MatchString(key) && key != "." && key != ".." && !strings.HasPrefix(key, "..")
}

func (v *validator) outputs(s *v1alpha1.LogtoConfigSpec) {
	if s.Outputs == nil {
		return
	}
	ids, secrets := Outputs(s)
	published := map[string]string{} // key -> target path, across both targets
	check := func(path string, t *v1alpha1.OutputTarget, own, other map[string]bool, otherKind, otherPath string) {
		if t == nil {
			return
		}
		if len(t.Keys) == 0 {
			for _, name := range sortedKeys(own) {
				switch {
				case !validKey(name):
					v.add(path, "the output %q is no valid ConfigMap key: name it with keys", name)
				case published[name] != "":
					v.add(path, "the output %q is also published by %s; with envFrom one would hide the other", name, published[name])
				}
				published[name] = path
			}
			return
		}
		for _, key := range sortedKeys(t.Keys) {
			p, name := path+".keys."+key, t.Keys[key]
			switch {
			case !validKey(key):
				v.add(p, "not a valid ConfigMap or Secret key (letters, digits, '-', '_', '.'; not . or ..)")
			case published[key] != "":
				v.add(p, "also published by %s; with envFrom one would hide the other", published[key])
			case own[name]:
			case other[name]:
				v.add(p, "%q is %s: publish it in %s", name, otherKind, otherPath)
			default:
				v.add(p, "%q is no output of this document", name)
			}
			published[key] = path
		}
	}
	check("spec.outputs.configMap", s.Outputs.ConfigMap, ids, secrets, "a secret", "spec.outputs.secret")
	check("spec.outputs.secret", s.Outputs.Secret, secrets, ids, "an identifier", "spec.outputs.configMap")
}

// Source is a valueFrom of a document, by path.
type Source struct {
	Path string
	From v1alpha1.ValueFrom
}

// Sources are every valueFrom a document reads (credentials, connector auth), sorted by path.
func Sources(s *v1alpha1.LogtoConfigSpec) []Source {
	var out []Source
	add := func(path string, f *v1alpha1.ValueFrom) {
		if f != nil {
			out = append(out, Source{Path: path + ".valueFrom", From: *f})
		}
	}
	add("spec.credentials.clientId", s.Credentials.ClientID.ValueFrom)
	add("spec.credentials.clientSecret", &s.Credentials.ClientSecret.ValueFrom)
	for _, k := range sortedKeys(s.Connectors) {
		if c := s.Connectors[k]; c.SMTP != nil {
			add("spec.connectors."+k+".smtp.auth.user", c.SMTP.Auth.User.ValueFrom)
			add("spec.connectors."+k+".smtp.auth.pass", &c.SMTP.Auth.Pass.ValueFrom)
		}
	}
	return out
}
