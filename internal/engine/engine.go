// Package engine reconciles a LogtoConfig with a Logto, kind by kind, in dependency order. It knows nothing of the
// CLI or of Kubernetes: both call Plan and Apply.
//
//   - Plan checks first that the target accepts every field the document will send (capability), then asks each kind
//     to observe Logto and diff it with the document. It writes nothing.
//   - Apply does the same, kind by kind, runs the kind's creates and updates, then plans that kind again and fails if
//     Logto did not keep what was written. Deletes (prune) run last, kinds in reverse order, so that nothing is
//     deleted while a declared object still points at it. A replay of an unchanged document writes nothing.
//
// A failed apply returns the plan it ran with each op's Result, and an *OpError; Transient says whether a retry may
// succeed.
package engine

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strings"
	"syscall"

	logtospecs "github.com/Aliud-Corp/logto-operator/api/logto"
	"github.com/Aliud-Corp/logto-operator/api/v1alpha1"
	"github.com/Aliud-Corp/logto-operator/internal/engine/plan"
	"github.com/Aliud-Corp/logto-operator/internal/logto/capability"
	"github.com/Aliud-Corp/logto-operator/internal/logto/client"
	"github.com/Aliud-Corp/logto-operator/internal/secrets"
)

// Kind reconciles one family of Logto objects. Implementations live in internal/engine/kinds.
type Kind interface {
	// Name is the kind's name in the document and the plan ("resources", "applications", ...).
	Name() string
	// DependsOn names the kinds whose refs (ids) this kind reads.
	DependsOn() []string
	// Needs lists what the kind will send, for the capability check.
	Needs(env *Env) ([]capability.Need, error)
	// Plan observes Logto and returns one op per declared object (and per pruned one). It records in env.Refs the
	// ids of the objects that already exist.
	Plan(ctx context.Context, env *Env) ([]plan.Op, error)
	// Apply runs one op; it records in env.Refs the ids of the objects it creates.
	Apply(ctx context.Context, env *Env, op plan.Op) error
}

// Env is what kinds work with during one run.
type Env struct {
	Client     *client.Client
	Spec       *v1alpha1.LogtoConfigSpec // loaded and validated, files resolved
	Secrets    secrets.Resolver
	Refs       *Refs
	Capability *capability.Spec // the target's own spec; required unless SkipCapability
	// SkipCapability runs without the capability check: tests of kinds against the fake only.
	SkipCapability bool
	// AllowUntested runs against a Logto whose spec is none of the supported versions (Capability.Version empty):
	// its behaviour was never tested. The CLI's --allow-untested-logto, later a field of the CR.
	AllowUntested bool
	Outputs       *Outputs
}

// Publisher is a kind that publishes outputs read only during an apply (secret values), once every op has run: a
// plan never reads them.
type Publisher interface {
	Publish(ctx context.Context, env *Env, ops []plan.Op) error
}

// ErrUntested is the refusal of a Logto outside the support window (Env.AllowUntested lifts it).
var ErrUntested = errors.New("untested Logto")

// OpError is the failure of one op.
type OpError struct {
	Kind, Key string
	Err       error
}

func (e *OpError) Error() string { return e.Kind + " " + e.Key + ": " + e.Err.Error() }
func (e *OpError) Unwrap() error { return e.Err }

// Transient reports whether an error may go away on a retry: Logto (or its token endpoint) overloaded (429), failing
// (5xx) or unreachable. A refusal (4xx), a certificate Logto's TLS does not pass, an invalid document or a write
// Logto did not keep is not: retrying changes nothing.
func Transient(err error) bool {
	var api *client.APIError
	if errors.As(err, &api) {
		return api.Status == 429 || api.Status >= 500
	}
	var auth *client.AuthError
	if errors.As(err, &auth) {
		return auth.Status == 429 || auth.Status >= 500
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	var unknownCA x509.UnknownAuthorityError
	var invalid x509.CertificateInvalidError
	var hostname x509.HostnameError
	var verify *tls.CertificateVerificationError
	if errors.As(err, &unknownCA) || errors.As(err, &invalid) || errors.As(err, &hostname) || errors.As(err, &verify) {
		return false
	}
	// Transport: a timeout, a connection refused, reset or cut (a plain EOF: Logto or its ingress restarted mid-call),
	// a DNS failure that is not "no such host". Anything
	// else (an unknown host, an https endpoint on an http port) is a configuration error.
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return dns.IsTimeout || dns.IsTemporary
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	for _, e := range []error{syscall.ECONNREFUSED, syscall.ECONNRESET, syscall.ECONNABORTED, syscall.EPIPE, io.EOF, io.ErrUnexpectedEOF} {
		if errors.Is(err, e) {
			return true
		}
	}
	var op *net.OpError
	return errors.As(err, &op) && op.Op == "dial"
}

// Engine runs kinds in dependency order.
type Engine struct {
	kinds []Kind
}

// New orders the kinds by DependsOn (registration order among independent kinds). A missing dependency or a cycle
// is an error.
func New(kinds ...Kind) (*Engine, error) {
	byName := map[string]Kind{}
	for _, k := range kinds {
		if _, dup := byName[k.Name()]; dup {
			return nil, fmt.Errorf("kind %s registered twice", k.Name())
		}
		byName[k.Name()] = k
	}
	var ordered []Kind
	state := map[string]int{} // 0 unseen, 1 visiting, 2 done
	var visit func(k Kind, chain []string) error
	visit = func(k Kind, chain []string) error {
		switch state[k.Name()] {
		case 1:
			return fmt.Errorf("kinds depend on each other in a cycle: %s", strings.Join(append(chain, k.Name()), " -> "))
		case 2:
			return nil
		}
		state[k.Name()] = 1
		for _, dep := range k.DependsOn() {
			d, ok := byName[dep]
			if !ok {
				return fmt.Errorf("kind %s depends on %s, which is not registered", k.Name(), dep)
			}
			if err := visit(d, append(chain, k.Name())); err != nil {
				return err
			}
		}
		state[k.Name()] = 2
		ordered = append(ordered, k)
		return nil
	}
	for _, k := range kinds {
		if err := visit(k, nil); err != nil {
			return nil, err
		}
	}
	return &Engine{kinds: ordered}, nil
}

// Kinds are the kind names in reconciliation order.
func (e *Engine) Kinds() []string {
	out := make([]string, len(e.kinds))
	for i, k := range e.kinds {
		out[i] = k.Name()
	}
	return out
}

func (e *Engine) checkCapabilities(env *Env) error {
	if env.SkipCapability {
		return nil
	}
	if env.Capability == nil {
		return errors.New("no capability spec: the target's own API spec is required before any call")
	}
	if env.Capability.Version == "" && !env.AllowUntested {
		return fmt.Errorf("%w: its API spec is none of the supported versions (%s), its behaviour is untested; "+
			"run a supported Logto, or allow an untested one", ErrUntested, strings.Join(logtospecs.Versions(), ", "))
	}
	var needs []capability.Need
	for _, k := range e.kinds {
		n, err := k.Needs(env)
		if err != nil {
			return fmt.Errorf("%s: %w", k.Name(), err)
		}
		needs = append(needs, n...)
	}
	return env.Capability.Check(needs...)
}

// Plan returns what Apply would do, writing nothing.
func (e *Engine) Plan(ctx context.Context, env *Env) (*plan.Plan, error) {
	env.init()
	if err := e.checkCapabilities(env); err != nil {
		return nil, err
	}
	p := &plan.Plan{}
	for _, k := range e.kinds {
		ops, err := k.Plan(ctx, env)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", k.Name(), err)
		}
		p.Add(k.Name(), ops...)
	}
	return p, nil
}

// Apply reconciles every kind and returns the plan it ran, each op with its Result. It stops at the first failure:
// what was applied stays applied (a later run resumes from Logto's state, there is no state file to repair).
func (e *Engine) Apply(ctx context.Context, env *Env) (*plan.Plan, error) {
	env.init()
	if err := e.checkCapabilities(env); err != nil {
		return nil, err
	}
	done := &plan.Plan{}
	spans := make([][2]int, len(e.kinds)) // each kind's ops in done.Ops
	creatingOrUpdating := func(a plan.Action) bool { return a == plan.Create || a == plan.Update }
	for i, k := range e.kinds {
		ops, err := k.Plan(ctx, env)
		if err != nil {
			return done, fmt.Errorf("%s: %w", k.Name(), err)
		}
		start := len(done.Ops)
		done.Add(k.Name(), ops...)
		spans[i] = [2]int{start, len(done.Ops)}
		if err := e.run(ctx, env, k, done.Ops[start:], creatingOrUpdating); err != nil {
			return done, err
		}
	}
	for i := len(e.kinds) - 1; i >= 0; i-- {
		s := spans[i]
		if err := e.run(ctx, env, e.kinds[i], done.Ops[s[0]:s[1]], func(a plan.Action) bool { return a == plan.Delete }); err != nil {
			return done, err
		}
	}
	for i, k := range e.kinds {
		if pub, ok := k.(Publisher); ok {
			if err := pub.Publish(ctx, env, done.Ops[spans[i][0]:spans[i][1]]); err != nil {
				return done, fmt.Errorf("%s: publish: %w", k.Name(), err)
			}
		}
	}
	return done, nil
}

// run applies the ops of one kind that pass the filter, then reads the kind back: every op already run must now
// plan as a no-op (deletes still to run are ignored).
func (e *Engine) run(ctx context.Context, env *Env, k Kind, ops []plan.Op, filter func(plan.Action) bool) error {
	touched := 0
	for i := range ops {
		if !filter(ops[i].Action) {
			continue
		}
		if err := k.Apply(ctx, env, ops[i]); err != nil {
			ops[i].Result = plan.Failed
			return &OpError{Kind: k.Name(), Key: ops[i].Key, Err: err}
		}
		ops[i].Result = plan.Applied
		touched++
	}
	if touched == 0 {
		return nil
	}
	again, err := k.Plan(ctx, env)
	if err != nil {
		return fmt.Errorf("%s: read back: %w", k.Name(), err)
	}
	var pending []plan.Op
	for _, op := range again {
		if op.Action == plan.Delete && !filter(plan.Delete) {
			continue
		}
		pending = append(pending, op)
	}
	return notKept(k.Name(), pending)
}

func notKept(kind string, ops []plan.Op) error {
	var errs []error
	for _, op := range ops {
		if op.Action == plan.Noop {
			continue
		}
		var fields []string
		for _, c := range op.Changes {
			fields = append(fields, c.Field)
		}
		sort.Strings(fields)
		errs = append(errs, fmt.Errorf("%s %s: Logto did not keep what was written (%s)", kind, op.Key, strings.Join(fields, ", ")))
	}
	return errors.Join(errs...)
}

func (env *Env) init() {
	if env.Refs == nil {
		env.Refs = &Refs{}
	}
	if env.Outputs == nil {
		env.Outputs = &Outputs{}
	}
}

// Refs are the Logto-generated values (ids) of declared objects, by kind, local key and field, as learnt during a
// run. A ref of an object the same plan creates is unknown until Apply.
type Refs struct {
	m map[string]string
}

func refKey(kind, key, field string) string { return kind + "\x00" + key + "\x00" + field }

// Set records a ref.
func (r *Refs) Set(kind, key, field, value string) {
	if r.m == nil {
		r.m = map[string]string{}
	}
	r.m[refKey(kind, key, field)] = value
}

// Get returns a ref and whether it is known.
func (r *Refs) Get(kind, key, field string) (string, bool) {
	v, ok := r.m[refKey(kind, key, field)]
	return v, ok
}

// Value returns a ref, or plan.KnownAfterApply when it is not known yet (for rendering a plan).
func (r *Refs) Value(kind, key, field string) string {
	if v, ok := r.Get(kind, key, field); ok {
		return v
	}
	return plan.KnownAfterApply
}

// Outputs are the values a run publishes: identifiers (printable) and secrets (never printed).
type Outputs struct {
	IDs     map[string]string
	Secrets map[string]secrets.Secret
}

// SetID records a printable output.
func (o *Outputs) SetID(name, value string) {
	if o.IDs == nil {
		o.IDs = map[string]string{}
	}
	o.IDs[name] = value
}

// SetSecret records a secret output.
func (o *Outputs) SetSecret(name string, value secrets.Secret) {
	if o.Secrets == nil {
		o.Secrets = map[string]secrets.Secret{}
	}
	o.Secrets[name] = value
}
