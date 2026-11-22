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
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/roles"
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
func (Kind) DependsOn() []string { return []string{roles.Name} }

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
		if d.Secret != nil && d.Secret.Publish {
			need, err := kindkit.Need("CreateApplicationSecret", where+".secret", createSecret{Name: SecretName})
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
	grant   []roles.Grant
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
	held, err := heldRoles(ctx, env)
	if err != nil {
		return nil, err
	}
	// The grants and the secret, from the declaration alone.
	intent := func(key string, d v1alpha1.Application) ([]roles.Grant, bool, error) {
		grant, err := roles.Resolve(env, "spec.applications."+key, d.Roles, held, "MachineToMachine")
		return grant, d.Secret != nil && d.Secret.Publish, err
	}
	return kindkit.Collection[v1alpha1.Application, wireApp]{
		Kind: Name, Where: "spec.applications", Declared: env.Spec.Applications,
		Natural: func(_ string, d v1alpha1.Application) string { return d.Name },
		Held:    func(a wireApp) string { return a.Name },
		Create: func(key string, d v1alpha1.Application) ([]plan.Op, error) {
			grant, publish, err := intent(key, d)
			if err != nil {
				return nil, err
			}
			changes := []plan.Change{{Field: "type", To: d.Type}}
			for _, r := range d.Roles {
				changes = append(changes, plan.Change{Field: "roles." + r.Name, To: "granted"})
			}
			return []plan.Op{{
				Kind: Name, Key: key, Name: d.Name, Action: plan.Create, Changes: changes,
				Payload: payload{key: key, d: d, create: true, grant: grant, publish: publish},
			}}, nil
		},
		Update: func(key string, d v1alpha1.Application, have wireApp) ([]plan.Op, error) {
			grant, publish, err := intent(key, d)
			if err != nil {
				return nil, err
			}
			op, err := updateOp(ctx, env, "spec.applications."+key, key, d, have, grant, publish)
			if err != nil {
				return nil, err
			}
			return []plan.Op{op}, nil
		},
	}.Plan(observed)
}

func updateOp(ctx context.Context, env *engine.Env, path, key string, d v1alpha1.Application, have wireApp, grant []roles.Grant, publish bool) (plan.Op, error) {
	if have.Type != d.Type {
		return plan.Op{}, fmt.Errorf("%s: the application exists with type %s; Logto cannot change an application's type (declared %s)", path, have.Type, d.Type)
	}
	env.Refs.Set(Name, key, RefID, have.ID)
	env.Outputs.SetID(OutputID(key), have.ID)
	p := payload{key: key, id: have.ID, d: d, publish: publish}
	var changes []plan.Change
	if d.Description != "" {
		changes = kindkit.Field(changes, "description", kindkit.Deref(have.Description), d.Description)
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
		for _, g := range grant {
			if g.ID == "" || !granted[g.ID] {
				p.grant = append(p.grant, g)
				changes = append(changes, plan.Change{Field: "roles." + g.Name, To: "granted"})
			}
		}
	}
	if publish {
		// The plan only learns whether a usable secret exists; its value is read by Publish, during an apply.
		_, ok, err := usableSecret(ctx, env, have.ID, false)
		if err != nil {
			return plan.Op{}, err
		}
		if !ok {
			changes = append(changes, plan.Change{Field: "secret", To: "created", Sensitive: true})
		}
	}
	return plan.Op{Kind: Name, Key: key, Name: d.Name, Action: kindkit.Action(changes), Changes: changes, Payload: p}, nil
}

// heldRoles are the roles Logto holds, by name, when an application grants any.
func heldRoles(ctx context.Context, env *engine.Env) (map[string]roles.Held, error) {
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
	out := map[string]roles.Held{}
	for _, r := range list {
		out[r.Name] = roles.Held{ID: r.ID, Type: r.Type}
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

// usableSecret finds a secret that has not expired, preferring Logto's "Default secret"; it returns its value only
// when reveal is set (an apply), registered with the redactor.
func usableSecret(ctx context.Context, env *engine.Env, id string, reveal bool) (secrets.Secret, bool, error) {
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
	if !reveal {
		return secrets.Secret{}, true, nil
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
		ids, err := roles.IDs(env, p.grant)
		if err != nil {
			return err
		}
		if err := kindkit.Send(ctx, env, assignRoles{RoleIDs: ids}, nil, func(s, ct string, b *bytes.Reader) (*http.Request, error) {
			return mgmt.NewAssignApplicationRolesRequestWithBody(s, p.id, ct, b)
		}); err != nil {
			return err
		}
	}
	if !p.publish {
		return nil
	}
	sec, ok, err := usableSecret(ctx, env, p.id, true)
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

// Publish publishes, after an apply, the secret of every application declared with secret.publish whose op did not
// already publish one (an unchanged application, or one changed without a new secret).
func (Kind) Publish(ctx context.Context, env *engine.Env, ops []plan.Op) error {
	for _, op := range ops {
		p, ok := op.Payload.(payload)
		if !ok || !p.publish || p.id == "" {
			continue
		}
		if _, done := env.Outputs.Secrets[OutputSecret(p.key)]; done {
			continue
		}
		sec, found, err := usableSecret(ctx, env, p.id, true)
		if err != nil {
			return err
		}
		if found {
			env.Outputs.SetSecret(OutputSecret(p.key), sec)
		}
	}
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
