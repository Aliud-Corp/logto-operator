// Package connectors reconciles passwordless connectors; M1 supports the SMTP e-mail connector. A connector is
// matched by its declared id, else adopted when it is the only one of its factory. Logto validates a connector's
// config whole and returns it with its secrets in clear (measured, test/integration/connector_probe_test.go): the
// kind always sends the whole config and compares the password like any field, masked in the plan.
package connectors

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"

	"github.com/Aliud-Corp/logto-operator/api/v1alpha1"
	"github.com/Aliud-Corp/logto-operator/internal/engine"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindkit"
	"github.com/Aliud-Corp/logto-operator/internal/engine/plan"
	"github.com/Aliud-Corp/logto-operator/internal/logto/capability"
	"github.com/Aliud-Corp/logto-operator/internal/logto/mgmt"
)

// Name of the kind.
const Name = "connectors"

// RefID is the ref field of a connector's id; RefEmail is set on any key whose connector sends e-mail.
const (
	RefID    = "id"
	RefEmail = "email"
)

// SMTPFactory is the factory id of the SMTP connector.
const SMTPFactory = "simple-mail-transfer-protocol"

// Kind reconciles spec.connectors.
type Kind struct{}

func (Kind) Name() string        { return Name }
func (Kind) DependsOn() []string { return nil }

// Needs lists the bodies the kind can send; config is passed to the connector, which validates it.
func (Kind) Needs(env *engine.Env) ([]capability.Need, error) {
	var needs []capability.Need
	for _, key := range kindkit.Keys(env.Spec.Connectors) {
		c := env.Spec.Connectors[key]
		for op, body := range map[string]any{
			"CreateConnector": createConnector{ID: c.ID, ConnectorID: c.ConnectorID, Config: smtpConfig{}},
			"UpdateConnector": updateConnector{Config: smtpConfig{}},
		} {
			need, err := kindkit.Need(op, "spec.connectors."+key, body)
			if err != nil {
				return nil, err
			}
			need.Fields = capability.Prefix(need.Fields, "config")
			needs = append(needs, need)
		}
	}
	return append(needs, kindkit.Deletes(env.Spec.Prune.Connectors, "spec.prune.connectors", "DeleteConnector")...), nil
}

type payload struct {
	id             string
	create, delete bool
	body           any
}

// Plan observes the connectors.
func (Kind) Plan(ctx context.Context, env *engine.Env) ([]plan.Op, error) {
	req, err := mgmt.NewListConnectorsRequest(env.Client.Endpoint(), &mgmt.ListConnectorsParams{})
	if err != nil {
		return nil, err
	}
	var observed []wireConnector
	if err := env.Client.Do(ctx, req, &observed); err != nil {
		return nil, err
	}
	byID := map[string]wireConnector{}
	byFactory := map[string][]wireConnector{}
	for _, c := range observed {
		byID[c.ID] = c
		byFactory[c.ConnectorID] = append(byFactory[c.ConnectorID], c)
	}
	matched := map[string]bool{}
	var ops []plan.Op
	for _, key := range kindkit.Keys(env.Spec.Connectors) {
		d := env.Spec.Connectors[key]
		path := "spec.connectors." + key
		want, err := desiredConfig(env, path, d)
		if err != nil {
			return nil, err
		}
		have, exists, err := match(path, d, byID, byFactory)
		if err != nil {
			return nil, err
		}
		env.Refs.Set(Name, key, RefEmail, "true")
		if !exists {
			ops = append(ops, plan.Op{
				Kind: Name, Key: key, Name: d.ConnectorID, Action: plan.Create,
				Changes: []plan.Change{{Field: "connectorId", To: d.ConnectorID}, {Field: "config.host", To: want.Host}},
				Payload: payload{create: true, body: createConnector{ID: d.ID, ConnectorID: d.ConnectorID, Config: want}},
			})
			continue
		}
		matched[have.ID] = true
		env.Refs.Set(Name, key, RefID, have.ID)
		next, err := body(want, have.Config)
		if err != nil {
			return nil, err
		}
		changes := diff(next, have.Config)
		ops = append(ops, plan.Op{
			Kind: Name, Key: key, Name: d.ConnectorID, Action: kindkit.Action(changes), Changes: changes,
			Payload: payload{id: have.ID, body: updateConnector{Config: next}},
		})
	}
	if env.Spec.Prune.Connectors {
		for _, c := range observed {
			// Only e-mail connectors of the factories this kind manages are pruned; social connectors are not ours.
			if matched[c.ID] || c.ConnectorID != SMTPFactory || kindkit.Kept(env, c.ID) {
				continue
			}
			ops = append(ops, plan.Op{Kind: Name, Key: c.ID, Name: c.ConnectorID, Action: plan.Delete, Payload: payload{id: c.ID, delete: true}})
		}
	}
	return ops, nil
}

func match(path string, d v1alpha1.Connector, byID map[string]wireConnector, byFactory map[string][]wireConnector) (wireConnector, bool, error) {
	if d.ID != "" {
		c, ok := byID[d.ID]
		if ok && c.ConnectorID != d.ConnectorID {
			return c, false, fmt.Errorf("%s: connector %s exists with the factory %s, not %s", path, d.ID, c.ConnectorID, d.ConnectorID)
		}
		return c, ok, nil
	}
	switch list := byFactory[d.ConnectorID]; len(list) {
	case 0:
		return wireConnector{}, false, nil
	case 1:
		return list[0], true, nil
	default:
		return wireConnector{}, false, fmt.Errorf("%s: %d connectors of the factory %s exist; declare the id of the one to manage", path, len(list), d.ConnectorID)
	}
}

func desiredConfig(env *engine.Env, path string, d v1alpha1.Connector) (smtpConfig, error) {
	s := d.SMTP
	if s == nil {
		return smtpConfig{}, fmt.Errorf("%s: only the SMTP connector is supported", path)
	}
	user, err := env.Secrets.String(path+".smtp.auth.user", s.Auth.User)
	if err != nil {
		return smtpConfig{}, err
	}
	pass, err := env.Secrets.Secret(path+".smtp.auth.pass", s.Auth.Pass)
	if err != nil {
		return smtpConfig{}, err
	}
	c := smtpConfig{
		Host: s.Host, Port: int(s.Port), Secure: s.Secure, IgnoreTLS: s.IgnoreTLS, FromEmail: s.FromEmail, ReplyTo: s.ReplyTo,
		Auth: smtpAuth{User: user, Pass: pass.Reveal()},
	}
	for _, t := range s.Templates.Items {
		c.Templates = append(c.Templates, smtpTemplate{UsageType: t.UsageType, ContentType: t.ContentType, Subject: t.Subject, Content: t.Content.Value})
	}
	sort.Slice(c.Templates, func(i, j int) bool { return c.Templates[i].UsageType < c.Templates[j].UsageType })
	return c, nil
}

// body is the whole config to send: the declared one over what Logto holds (a key the kind does not manage
// survives), an undeclared replyTo removed.
func body(want smtpConfig, have map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(want)
	if err != nil {
		return nil, err
	}
	var w map[string]any
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil, err
	}
	out, _ := kindkit.Overlay(have, w).(map[string]any)
	if want.ReplyTo == "" {
		delete(out, "replyTo")
	}
	return out, nil
}

// diff compares the config to send with Logto's, key by key; the password is a sensitive change.
func diff(next, have map[string]any) []plan.Change {
	if t, ok := have["templates"].([]any); ok {
		sort.SliceStable(t, func(i, j int) bool {
			if usage(t[i]) != usage(t[j]) {
				return usage(t[i]) < usage(t[j])
			}
			return content(t[i]) < content(t[j])
		})
	}
	keys := map[string]bool{}
	for k := range next {
		keys[k] = true
	}
	for k := range have {
		keys[k] = true
	}
	var changes []plan.Change
	for _, k := range kindkit.Keys(keys) {
		if k == "auth" {
			wa, _ := next["auth"].(map[string]any)
			ha, _ := have["auth"].(map[string]any)
			if fmt.Sprint(wa["user"]) != fmt.Sprint(ha["user"]) {
				changes = append(changes, plan.Change{Field: "config.auth.user", From: fmt.Sprint(ha["user"]), To: fmt.Sprint(wa["user"])})
			}
			if fmt.Sprint(wa["pass"]) != fmt.Sprint(ha["pass"]) {
				changes = append(changes, plan.Change{Field: "config.auth.pass", From: plan.Masked, To: plan.Masked, Sensitive: true})
			}
			continue
		}
		if k == "templates" {
			changes = append(changes, templateChanges(next[k], have[k])...)
			continue
		}
		if !kindkit.Equal(next[k], have[k]) {
			changes = append(changes, plan.Change{Field: "config." + k, From: kindkit.JSON(have[k]), To: kindkit.JSON(next[k])})
		}
	}
	return changes
}

// templateChanges compares the templates by usage type (and content type when Logto holds several for one usage
// type), field by field: a template added or removed is one change, a changed one shows only its changed fields.
func templateChanges(next, have any) []plan.Change {
	if kindkit.Equal(next, have) {
		return nil
	}
	split := splitUsages(next, have)
	n, okN := byUsage(next, split)
	h, okH := byUsage(have, split)
	if !okN || !okH {
		return []plan.Change{{Field: "config.templates", From: kindkit.JSON(have), To: kindkit.JSON(next)}}
	}
	all := map[string]bool{}
	for u := range n {
		all[u] = true
	}
	for u := range h {
		all[u] = true
	}
	var changes []plan.Change
	for _, u := range kindkit.Keys(all) {
		field := "config.templates[" + u + "]"
		wt, inN := n[u]
		ht, inH := h[u]
		switch {
		case !inH:
			changes = append(changes, plan.Change{Field: field, To: kindkit.JSON(wt)})
		case !inN:
			changes = append(changes, plan.Change{Field: field, From: kindkit.JSON(ht)})
		default:
			keys := map[string]bool{}
			for k := range wt {
				keys[k] = true
			}
			for k := range ht {
				keys[k] = true
			}
			for _, k := range kindkit.Keys(keys) {
				if !kindkit.Equal(wt[k], ht[k]) {
					changes = append(changes, plan.Change{Field: field + "." + k, From: text(ht[k]), To: text(wt[k])})
				}
			}
		}
	}
	return changes
}

// splitUsages names the usage types that come with more than one content type across both lists: their templates
// are keyed by usage type and content type, the others by usage type alone.
func splitUsages(lists ...any) map[string]bool {
	types := map[string]map[string]bool{}
	for _, v := range lists {
		list, _ := v.([]any)
		for _, t := range list {
			u := usage(t)
			if types[u] == nil {
				types[u] = map[string]bool{}
			}
			types[u][content(t)] = true
		}
	}
	split := map[string]bool{}
	for u, c := range types {
		split[u] = len(c) > 1
	}
	return split
}

// byUsage indexes templates by their key (see splitUsages); false when they are not a list of objects with distinct
// keys.
func byUsage(v any, split map[string]bool) (map[string]map[string]any, bool) {
	list, ok := v.([]any)
	if !ok && v != nil {
		return nil, false
	}
	out := map[string]map[string]any{}
	for _, t := range list {
		m, ok := t.(map[string]any)
		u := usage(t)
		key := u
		if split[u] {
			key = u + " " + content(t)
		}
		if _, dup := out[key]; !ok || u == "" || dup {
			return nil, false
		}
		out[key] = m
	}
	return out, true
}

// text renders a template field: a string as it is, anything else as JSON.
func text(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if v == nil {
		return ""
	}
	return kindkit.JSON(v)
}

func usage(t any) string {
	m, _ := t.(map[string]any)
	s, _ := m["usageType"].(string)
	return s
}

func content(t any) string {
	m, _ := t.(map[string]any)
	s, _ := m["contentType"].(string)
	return s
}

// Apply runs one op.
func (Kind) Apply(ctx context.Context, env *engine.Env, op plan.Op) error {
	p := op.Payload.(payload)
	switch {
	case p.delete:
		req, err := mgmt.NewDeleteConnectorRequest(env.Client.Endpoint(), p.id)
		if err != nil {
			return err
		}
		return env.Client.Do(ctx, req, nil)
	case p.create:
		var created wireConnector
		if err := kindkit.Send(ctx, env, p.body, &created, func(s, ct string, b *bytes.Reader) (*http.Request, error) {
			return mgmt.NewCreateConnectorRequestWithBody(s, ct, b)
		}); err != nil {
			return err
		}
		env.Refs.Set(Name, op.Key, RefID, created.ID)
		return nil
	default:
		return kindkit.Send(ctx, env, p.body, nil, func(s, ct string, b *bytes.Reader) (*http.Request, error) {
			return mgmt.NewUpdateConnectorRequestWithBody(s, p.id, ct, b)
		})
	}
}
