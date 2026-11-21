// Package profilefields reconciles the custom profile fields of sign-up, matched by name. The config of a field is
// passed to Logto as written; Logto may add defaults to it, so the comparison only asks that what the document
// declares be there.
package profilefields

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/Aliud-Corp/logto-operator/api/v1alpha1"
	"github.com/Aliud-Corp/logto-operator/internal/engine"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindkit"
	"github.com/Aliud-Corp/logto-operator/internal/engine/plan"
	"github.com/Aliud-Corp/logto-operator/internal/logto/capability"
	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

// Name of the kind.
const Name = "customProfileFields"

// Kind reconciles spec.customProfileFields.
type Kind struct{}

func (Kind) Name() string        { return Name }
func (Kind) DependsOn() []string { return nil }

// Needs lists the bodies the kind can send. config is free-form: its inner keys are Logto's to check.
func (Kind) Needs(env *engine.Env) ([]capability.Need, error) {
	var needs []capability.Need
	for _, key := range kindkit.Keys(env.Spec.CustomProfileFields) {
		d := env.Spec.CustomProfileFields[key]
		where := "spec.customProfileFields." + key
		for op, body := range map[string]any{"CreateCustomProfileField": newCreate(d), "UpdateCustomProfileFieldByName": newUpdate(d)} {
			need, err := kindkit.Need(op, where, body)
			if err != nil {
				return nil, err
			}
			need.Fields = capability.Prefix(need.Fields, "config")
			needs = append(needs, need)
		}
	}
	return needs, nil
}

type payload struct {
	name           string
	d              v1alpha1.CustomProfileField
	create, delete bool
}

// Plan observes the custom profile fields.
func (Kind) Plan(ctx context.Context, env *engine.Env) ([]plan.Op, error) {
	req, err := mgmt.NewListCustomProfileFieldsRequest(env.Client.Endpoint())
	if err != nil {
		return nil, err
	}
	var observed []wireField
	if err := env.Client.Do(ctx, req, &observed); err != nil {
		return nil, err
	}
	byName := map[string]wireField{}
	for _, f := range observed {
		byName[f.Name] = f
	}
	declared := map[string]bool{}
	var ops []plan.Op
	for _, key := range kindkit.Keys(env.Spec.CustomProfileFields) {
		d := env.Spec.CustomProfileFields[key]
		declared[d.Name] = true
		have, exists := byName[d.Name]
		if !exists {
			ops = append(ops, plan.Op{
				Kind: Name, Key: key, Name: d.Name, Action: plan.Create,
				Changes: []plan.Change{{Field: "type", To: d.Type}, {Field: "label", To: d.Label}},
				Payload: payload{name: d.Name, d: d, create: true},
			})
			continue
		}
		var changes []plan.Change
		changes = kindkit.Field(changes, "type", have.Type, d.Type)
		changes = kindkit.Field(changes, "label", have.Label, d.Label)
		if d.Description != "" {
			changes = kindkit.Field(changes, "description", kindkit.Deref(have.Description), d.Description)
		}
		changes = kindkit.Field(changes, "required", strconv.FormatBool(have.Required), strconv.FormatBool(d.Required))
		if d.Config != nil {
			var want any
			if err := json.Unmarshal(d.Config.Raw, &want); err != nil {
				return nil, err
			}
			if !kindkit.Subset(want, have.Config) {
				changes = append(changes, plan.Change{Field: "config", From: kindkit.JSON(have.Config), To: kindkit.JSON(want)})
			}
		}
		ops = append(ops, plan.Op{Kind: Name, Key: key, Name: d.Name, Action: kindkit.Action(changes), Changes: changes, Payload: payload{name: d.Name, d: d}})
	}
	if env.Spec.Prune.CustomProfileFields {
		for _, f := range observed {
			if declared[f.Name] || kindkit.Kept(env, f.Name) {
				continue
			}
			ops = append(ops, plan.Op{Kind: Name, Key: f.Name, Name: f.Name, Action: plan.Delete, Payload: payload{name: f.Name, delete: true}})
		}
	}
	return ops, nil
}

// Apply runs one op.
func (Kind) Apply(ctx context.Context, env *engine.Env, op plan.Op) error {
	p := op.Payload.(payload)
	switch {
	case p.delete:
		req, err := mgmt.NewDeleteCustomProfileFieldByNameRequest(env.Client.Endpoint(), p.name)
		if err != nil {
			return err
		}
		return env.Client.Do(ctx, req, nil)
	case p.create:
		return kindkit.Send(ctx, env, newCreate(p.d), nil, func(s, ct string, b *bytes.Reader) (*http.Request, error) {
			return mgmt.NewCreateCustomProfileFieldRequestWithBody(s, ct, b)
		})
	default:
		return kindkit.Send(ctx, env, newUpdate(p.d), nil, func(s, ct string, b *bytes.Reader) (*http.Request, error) {
			return mgmt.NewUpdateCustomProfileFieldByNameRequestWithBody(s, p.name, ct, b)
		})
	}
}
