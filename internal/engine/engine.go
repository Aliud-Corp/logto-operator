// Package engine reconciles a LogtoConfig with a Logto, kind by kind, in dependency order. It knows nothing of the
// CLI or of Kubernetes: both call Plan and Apply.
//
//   - Plan checks first that the target accepts every field the document will send (capability), then asks each kind
//     to observe Logto and diff it with the document. It writes nothing.
//   - Apply does the same, kind by kind, then runs the kind's non-no-op ops, then plans that kind again and fails if
//     Logto did not keep what was written. A replay of an unchanged document plans only no-ops and writes nothing.
package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

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
	Capability *capability.Spec // the target's own spec; nil skips the check (tests of a single kind)
	Outputs    *Outputs
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
	if env.Capability == nil {
		return nil
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

// Apply reconciles every kind in order and returns the plan it executed. It stops at the first failure: kinds
// already applied stay applied (a later run resumes from Logto's state, there is no state file to repair).
func (e *Engine) Apply(ctx context.Context, env *Env) (*plan.Plan, error) {
	env.init()
	if err := e.checkCapabilities(env); err != nil {
		return nil, err
	}
	done := &plan.Plan{}
	for _, k := range e.kinds {
		ops, err := k.Plan(ctx, env)
		if err != nil {
			return done, fmt.Errorf("%s: %w", k.Name(), err)
		}
		done.Add(k.Name(), ops...)
		touched := 0
		for _, op := range ops {
			if op.Action == plan.Noop {
				continue
			}
			if err := k.Apply(ctx, env, op); err != nil {
				return done, fmt.Errorf("%s %s: %w", k.Name(), op.Key, err)
			}
			touched++
		}
		if touched == 0 {
			continue
		}
		again, err := k.Plan(ctx, env)
		if err != nil {
			return done, fmt.Errorf("%s: read back: %w", k.Name(), err)
		}
		if err := notKept(k.Name(), again); err != nil {
			return done, err
		}
	}
	return done, nil
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
