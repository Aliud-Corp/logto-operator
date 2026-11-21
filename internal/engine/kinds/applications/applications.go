// Package applications reconciles applications, matched by name and never deleted or recreated: Logto generates
// their ids, which this kind publishes as outputs with the M2M secrets. Roles are granted, never revoked (a role
// granted by hand stays); a secret is created only when the application has no usable one, and never rotated.
package applications

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/Aliud-Corp/logto-operator/api/v1alpha1"
	"github.com/Aliud-Corp/logto-operator/internal/engine"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindkit"
	"github.com/Aliud-Corp/logto-operator/internal/engine/plan"
	"github.com/Aliud-Corp/logto-operator/internal/logto/capability"
	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
	"github.com/Aliud-Corp/logto-operator/internal/secrets"
)

// Name of the kind.
const Name = "applications"

// RefID is the ref field of an application's id.
const RefID = "id"

// SecretName is the name of a secret this kind creates.
const SecretName = "logto-operator"

// OutputID and OutputSecret name the outputs of an application.
func OutputID(key string) string     { return "applications." + key + ".id" }
func OutputSecret(key string) string { return "applications." + key + ".secret" }

// Kind reconciles spec.applications.
type Kind struct{}

func (Kind) Name() string        { return Name }
func (Kind) DependsOn() []string { return nil }

// Needs lists the bodies the kind can send.
func (Kind) Needs(env *engine.Env) ([]capability.Need, error) {
	var needs []capability.Need
	for _, key := range kindkit.Keys(env.Spec.Applications) {
		d := env.Spec.Applications[key]
		where := "spec.applications." + key
		for op, body := range map[string]any{"CreateApplication": newCreate(d), "UpdateApplication": newUpdate(d, wireApp{})} {
			need, err := kindkit.Need(op, where, body)
			if err != nil {
				return nil, err
			}
			needs = append(needs, need)
		}
		if len(d.Roles) > 0 {
			need, err := kindkit.Need("AssignApplicationRoles", where+".roles", assignRoles{RoleIDs: []string{}})
			if err != nil {
				return nil, err
			}
			needs = append(needs, need)
		}
	}
	return needs, nil
}

type payload struct {
	key     string
	id      string
	d       v1alpha1.Application
	create  bool
	update  *updateApp
	grant   []string // role ids to grant
	secret  bool     // a secret must be created
	publish bool
}

// Plan observes the applications.
func (Kind) Plan(ctx context.Context, env *engine.Env) ([]plan.Op, error) {
	if len(env.Spec.Applications) == 0 {
		return nil, nil
	}
	observed, err := kindkit.List[wireApp](ctx, env, func(p kindkit.Page) (*http.Request, error) {
		return mgmt.NewListApplicationsRequest(env.Client.Endpoint(), &mgmt.ListApplicationsParams{Page: p.Page, PageSize: p.Size})
	})
	if err != nil {
		return nil, err
	}
	byName := map[string][]wireApp{}
	for _, a := range observed {
		byName[a.Name] = append(byName[a.Name], a)
	}
	roles, err := roleIDs(ctx, env)
	if err != nil {
		return nil, err
	}
	var ops []plan.Op
	for _, key := range kindkit.Keys(env.Spec.Applications) {
		d := env.Spec.Applications[key]
		path := "spec.applications." + key
		grant, err := wanted(path, d, roles)
		if err != nil {
			return nil, err
		}
		publish := d.Secret != nil && d.Secret.Publish
		switch found := byName[d.Name]; len(found) {
		case 0:
			changes := []plan.Change{{Field: "type", To: d.Type}}
			for _, r := range d.Roles {
				changes = append(changes, plan.Change{Field: "roles." + r.Name, To: "granted"})
			}
			ops = append(ops, plan.Op{
				Kind: Name, Key: key, Name: d.Name, Action: plan.Create, Changes: changes,
				Payload: payload{key: key, d: d, create: true, grant: grant, publish: publish, secret: publish},
			})
		case 1:
			op, err := updateOp(ctx, env, path, key, d, found[0], grant, publish)
			if err != nil {
				return nil, err
			}
			ops = append(ops, op)
		default:
			return nil, fmt.Errorf("%s: %d Logto applications are named %q; rename the extra ones so the match is unique", path, len(found), d.Name)
		}
	}
	return ops, nil
}

func updateOp(ctx context.Context, env *engine.Env, path, key string, d v1alpha1.Application, have wireApp, grant []string, publish bool) (plan.Op, error) {
	if have.Type != d.Type {
		return plan.Op{}, fmt.Errorf("%s: the application exists with type %s; Logto cannot change an application's type (declared %s)", path, have.Type, d.Type)
	}
	env.Refs.Set(Name, key, RefID, have.ID)
	env.Outputs.SetID(OutputID(key), have.ID)
	p := payload{key: key, id: have.ID, d: d, publish: publish}
	var changes []plan.Change
	if d.Description != "" {
		changes = kindkit.Field(changes, "description", deref(have.Description), d.Description)
	}
	if m := d.OIDCClientMetadata; m != nil {
		if !plan.SameSet(strings(have.OIDC["redirectUris"]), m.RedirectURIs) {
			changes = append(changes, plan.Change{Field: "oidcClientMetadata.redirectUris", From: plan.List(strings(have.OIDC["redirectUris"])), To: plan.List(m.RedirectURIs)})
		}
		if !plan.SameSet(strings(have.OIDC["postLogoutRedirectUris"]), m.PostLogoutRedirectURIs) {
			changes = append(changes, plan.Change{Field: "oidcClientMetadata.postLogoutRedirectUris", From: plan.List(strings(have.OIDC["postLogoutRedirectUris"])), To: plan.List(m.PostLogoutRedirectURIs)})
		}
	}
	if want := customMap(d); len(want) > 0 && !kindkit.Subset(want, have.Custom) {
		changes = append(changes, plan.Change{Field: "customClientMetadata", From: kindkit.JSON(have.Custom), To: kindkit.JSON(want)})
	}
	if len(changes) > 0 {
		p.update = newUpdate(d, have)
	}
	if len(grant) > 0 {
		granted, err := applicationRoles(ctx, env, have.ID)
		if err != nil {
			return plan.Op{}, err
		}
		for i, id := range grant {
			if !granted[id] {
				p.grant = append(p.grant, id)
				changes = append(changes, plan.Change{Field: "roles." + d.Roles[i].Name, To: "granted"})
			}
		}
	}
	if publish {
		secret, ok, err := usableSecret(ctx, env, have.ID)
		if err != nil {
			return plan.Op{}, err
		}
		if ok {
			env.Outputs.SetSecret(OutputSecret(key), secret)
		} else {
			p.secret = true
			changes = append(changes, plan.Change{Field: "secret", To: "created", Sensitive: true})
		}
	}
	return plan.Op{Kind: Name, Key: key, Name: d.Name, Action: kindkit.Action(changes), Changes: changes, Payload: p}, nil
}

// wanted resolves the declared roles to ids; a role Logto does not have, or of the wrong type, stops the plan.
func wanted(path string, d v1alpha1.Application, roles map[string]wireRole) ([]string, error) {
	var ids []string
	for i, r := range d.Roles {
		role, ok := roles[r.Name]
		if !ok {
			return nil, fmt.Errorf("%s.roles[%d]: Logto has no role named %q", path, i, r.Name)
		}
		if role.Type != "MachineToMachine" {
			return nil, fmt.Errorf("%s.roles[%d]: %q is a %s role; an application takes MachineToMachine roles", path, i, r.Name, role.Type)
		}
		ids = append(ids, role.ID)
	}
	return ids, nil
}

func roleIDs(ctx context.Context, env *engine.Env) (map[string]wireRole, error) {
	need := false
	for _, a := range env.Spec.Applications {
		need = need || len(a.Roles) > 0
	}
	if !need {
		return nil, nil
	}
	list, err := kindkit.List[wireRole](ctx, env, func(p kindkit.Page) (*http.Request, error) {
		return mgmt.NewListRolesRequest(env.Client.Endpoint(), &mgmt.ListRolesParams{Page: p.Page, PageSize: p.Size})
	})
	if err != nil {
		return nil, err
	}
	out := map[string]wireRole{}
	for _, r := range list {
		out[r.Name] = r
	}
	return out, nil
}

func applicationRoles(ctx context.Context, env *engine.Env, id string) (map[string]bool, error) {
	list, err := kindkit.List[wireRole](ctx, env, func(p kindkit.Page) (*http.Request, error) {
		return mgmt.NewListApplicationRolesRequest(env.Client.Endpoint(), id, &mgmt.ListApplicationRolesParams{Page: p.Page, PageSize: p.Size})
	})
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, r := range list {
		out[r.ID] = true
	}
	return out, nil
}

// usableSecret returns a secret that has not expired, preferring Logto's "Default secret".
func usableSecret(ctx context.Context, env *engine.Env, id string) (secrets.Secret, bool, error) {
	req, err := mgmt.NewListApplicationSecretsRequest(env.Client.Endpoint(), id)
	if err != nil {
		return secrets.Secret{}, false, err
	}
	var list []wireSecret
	if err := env.Client.Do(ctx, req, &list); err != nil {
		return secrets.Secret{}, false, err
	}
	var found *wireSecret
	for i, s := range list {
		if s.ExpiresAt != nil && *s.ExpiresAt <= float64(time.Now().UnixMilli()) {
			continue
		}
		if found == nil || s.Name == "Default secret" {
			found = &list[i]
		}
	}
	if found == nil {
		return secrets.Secret{}, false, nil
	}
	sec := secrets.New(found.Value)
	if env.Secrets.Redactor != nil {
		env.Secrets.Redactor.Add(sec)
	}
	return sec, true, nil
}

// Apply runs one op.
func (Kind) Apply(ctx context.Context, env *engine.Env, op plan.Op) error {
	p := op.Payload.(payload)
	if p.create {
		var created wireApp
		if err := kindkit.Send(ctx, env, newCreate(p.d), &created, func(s, ct string, b *bytes.Reader) (*http.Request, error) {
			return mgmt.NewCreateApplicationRequestWithBody(s, ct, b)
		}); err != nil {
			return err
		}
		p.id = created.ID
		env.Refs.Set(Name, p.key, RefID, created.ID)
		env.Outputs.SetID(OutputID(p.key), created.ID)
	}
	if p.update != nil {
		if err := kindkit.Send(ctx, env, p.update, nil, func(s, ct string, b *bytes.Reader) (*http.Request, error) {
			return mgmt.NewUpdateApplicationRequestWithBody(s, p.id, ct, b)
		}); err != nil {
			return err
		}
	}
	if len(p.grant) > 0 {
		if err := kindkit.Send(ctx, env, assignRoles{RoleIDs: p.grant}, nil, func(s, ct string, b *bytes.Reader) (*http.Request, error) {
			return mgmt.NewAssignApplicationRolesRequestWithBody(s, p.id, ct, b)
		}); err != nil {
			return err
		}
	}
	if !p.publish {
		return nil
	}
	sec, ok, err := usableSecret(ctx, env, p.id)
	if err != nil {
		return err
	}
	if !ok {
		var created wireSecret
		if err := kindkit.Send(ctx, env, createSecret{Name: SecretName}, &created, func(s, ct string, b *bytes.Reader) (*http.Request, error) {
			return mgmt.NewCreateApplicationSecretRequestWithBody(s, p.id, ct, b)
		}); err != nil {
			return err
		}
		sec = secrets.New(created.Value)
		if env.Secrets.Redactor != nil {
			env.Secrets.Redactor.Add(sec)
		}
	}
	env.Outputs.SetSecret(OutputSecret(p.key), sec)
	return nil
}

func customMap(d v1alpha1.Application) map[string]any {
	if d.CustomClientMetadata == nil {
		return nil
	}
	b, _ := json.Marshal(d.CustomClientMetadata)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

func strings(v any) []string {
	var out []string
	if l, ok := v.([]any); ok {
		for _, x := range l {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
