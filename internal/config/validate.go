package config

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/Aliud-Corp/logto-operator/api/v1alpha1"
)

// RequiredSMTPTemplates are the usage types the SMTP connector refuses to be created without (measured on Logto
// 1.44.0 and 1.46.0: "Template with UsageType (Generic) should be provided!").
var RequiredSMTPTemplates = []string{"Register", "SignIn", "ForgotPassword", "Generic"}

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
	v.users(s)
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

// resourceScopes checks that each reference names a declared resource and one of its declared scopes.
func (v *validator) resourceScopes(s *v1alpha1.LogtoConfigSpec, path string, refs []v1alpha1.ResourceScopeRef) {
	for i, rs := range refs {
		p := fmt.Sprintf("%s[%d]", path, i)
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

func (v *validator) users(s *v1alpha1.LogtoConfigSpec) {
	uniqueBy(v, "spec.users", s.Users, "primaryEmail", func(u v1alpha1.User) string { return strings.ToLower(u.PrimaryEmail) })
	for _, k := range sortedKeys(s.Users) {
		if !strings.Contains(s.Users[k].PrimaryEmail, "@") {
			v.add("spec.users."+k+".primaryEmail", "must be an e-mail address")
		}
		v.grantable(s, "spec.users."+k, s.Users[k].Roles, "User", "a user")
	}
}
