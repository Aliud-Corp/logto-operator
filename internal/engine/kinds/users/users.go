// Package users reconciles application-level administrators, matched by primary e-mail (case-insensitive).
// They are created without a password (they set it through the sign-in experience), never pruned, and their roles
// are granted, never revoked. Users from sign-up are never declared.
package users

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/Aliud-Corp/logto-operator/api/v1alpha1"
	"github.com/Aliud-Corp/logto-operator/internal/engine"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindkit"
	"github.com/Aliud-Corp/logto-operator/internal/engine/plan"
	"github.com/Aliud-Corp/logto-operator/internal/logto/capability"
	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

// Name of the kind.
const Name = "users"

// OutputID names the output of a user's id.
func OutputID(key string) string { return "users." + key + ".id" }

// Kind reconciles spec.users.
type Kind struct{}

func (Kind) Name() string        { return Name }
func (Kind) DependsOn() []string { return nil }

// Needs lists the bodies the kind can send.
func (Kind) Needs(env *engine.Env) ([]capability.Need, error) {
	var needs []capability.Need
	for _, key := range kindkit.Keys(env.Spec.Users) {
		d := env.Spec.Users[key]
		where := "spec.users." + key
		for op, body := range map[string]any{
			"CreateUser": createUser{PrimaryEmail: d.PrimaryEmail, Name: d.Name}, "UpdateUser": updateUser{Name: d.Name},
			"AssignUserRoles": assignRoles{RoleIDs: []string{}},
		} {
			need, err := kindkit.Need(op, where, body)
			if err != nil {
				return nil, err
			}
			needs = append(needs, need)
		}
	}
	return needs, nil
}

type payload struct {
	key, id string
	d       v1alpha1.User
	create  bool
	rename  bool
	grant   []string
}

// Plan looks every declared user up by primary e-mail.
func (Kind) Plan(ctx context.Context, env *engine.Env) ([]plan.Op, error) {
	if len(env.Spec.Users) == 0 {
		return nil, nil
	}
	roles, err := userRoles(ctx, env)
	if err != nil {
		return nil, err
	}
	var ops []plan.Op
	for _, key := range kindkit.Keys(env.Spec.Users) {
		d := env.Spec.Users[key]
		path := "spec.users." + key
		var grantIDs []string
		for i, r := range d.Roles {
			role, ok := roles[r.Name]
			if !ok {
				return nil, fmt.Errorf("%s.roles[%d]: Logto has no User role named %q", path, i, r.Name)
			}
			grantIDs = append(grantIDs, role.ID)
		}
		have, exists, err := find(ctx, env, d.PrimaryEmail)
		if err != nil {
			return nil, err
		}
		if !exists {
			changes := []plan.Change{{Field: "primaryEmail", To: d.PrimaryEmail}}
			for _, r := range d.Roles {
				changes = append(changes, plan.Change{Field: "roles." + r.Name, To: "granted"})
			}
			ops = append(ops, plan.Op{
				Kind: Name, Key: key, Name: d.PrimaryEmail, Action: plan.Create, Changes: changes,
				Payload: payload{key: key, d: d, create: true, grant: grantIDs},
			})
			continue
		}
		env.Outputs.SetID(OutputID(key), have.ID)
		p := payload{key: key, id: have.ID, d: d}
		var changes []plan.Change
		if d.Name != "" && kindkit.Deref(have.Name) != d.Name {
			changes = append(changes, plan.Change{Field: "name", From: kindkit.Deref(have.Name), To: d.Name})
			p.rename = true
		}
		if len(grantIDs) > 0 {
			granted, err := grantedRoles(ctx, env, have.ID)
			if err != nil {
				return nil, err
			}
			for i, id := range grantIDs {
				if !granted[id] {
					p.grant = append(p.grant, id)
					changes = append(changes, plan.Change{Field: "roles." + d.Roles[i].Name, To: "granted"})
				}
			}
		}
		ops = append(ops, plan.Op{Kind: Name, Key: key, Name: d.PrimaryEmail, Action: kindkit.Action(changes), Changes: changes, Payload: p})
	}
	return ops, nil
}

// find uses Logto's search on primaryEmail (its default mode, which matches across case), then keeps the user whose
// e-mail is the declared one, ignoring case.
func find(ctx context.Context, env *engine.Env, email string) (wireUser, bool, error) {
	search := map[string]string{"search.primaryEmail": email}
	list, err := kindkit.List[wireUser](ctx, env, func(p kindkit.Page) (*http.Request, error) {
		return mgmt.NewListUsersRequest(env.Client.Endpoint(), &mgmt.ListUsersParams{Page: p.Page, PageSize: p.Size, SearchParams: &search})
	})
	if err != nil {
		return wireUser{}, false, err
	}
	for _, u := range list {
		if strings.EqualFold(kindkit.Deref(u.PrimaryEmail), email) {
			return u, true, nil
		}
	}
	return wireUser{}, false, nil
}

func userRoles(ctx context.Context, env *engine.Env) (map[string]wireRole, error) {
	need := false
	for _, u := range env.Spec.Users {
		need = need || len(u.Roles) > 0
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
		if r.Type == "User" {
			out[r.Name] = r
		}
	}
	return out, nil
}

func grantedRoles(ctx context.Context, env *engine.Env, id string) (map[string]bool, error) {
	list, err := kindkit.List[wireRole](ctx, env, func(p kindkit.Page) (*http.Request, error) {
		return mgmt.NewListUserRolesRequest(env.Client.Endpoint(), id, &mgmt.ListUserRolesParams{Page: p.Page, PageSize: p.Size})
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

// Apply runs one op.
func (Kind) Apply(ctx context.Context, env *engine.Env, op plan.Op) error {
	p := op.Payload.(payload)
	if p.create {
		var created wireUser
		if err := kindkit.Send(ctx, env, createUser{PrimaryEmail: p.d.PrimaryEmail, Name: p.d.Name}, &created, func(s, ct string, b *bytes.Reader) (*http.Request, error) {
			return mgmt.NewCreateUserRequestWithBody(s, ct, b)
		}); err != nil {
			return err
		}
		p.id = created.ID
		env.Outputs.SetID(OutputID(p.key), created.ID)
	}
	if p.rename {
		if err := kindkit.Send(ctx, env, updateUser{Name: p.d.Name}, nil, func(s, ct string, b *bytes.Reader) (*http.Request, error) {
			return mgmt.NewUpdateUserRequestWithBody(s, p.id, ct, b)
		}); err != nil {
			return err
		}
	}
	if len(p.grant) > 0 {
		return kindkit.Send(ctx, env, assignRoles{RoleIDs: p.grant}, nil, func(s, ct string, b *bytes.Reader) (*http.Request, error) {
			return mgmt.NewAssignUserRolesRequestWithBody(s, p.id, ct, b)
		})
	}
	return nil
}
