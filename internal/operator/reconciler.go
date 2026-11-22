// Package operator reconciles LogtoConfig objects with the engine of logtoctl: one plan (Observe) or one apply
// (Apply) per reconcile, its outcome written as conditions, the generated ids and secrets published in a ConfigMap
// and a Secret of the document's namespace.
package operator

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/Aliud-Corp/logto-operator/api/v1alpha1"
	"github.com/Aliud-Corp/logto-operator/internal/config"
	"github.com/Aliud-Corp/logto-operator/internal/engine"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds"
	"github.com/Aliud-Corp/logto-operator/internal/engine/plan"
	"github.com/Aliud-Corp/logto-operator/internal/secrets"
	"github.com/Aliud-Corp/logto-operator/internal/target"
)

// Condition types.
const (
	Ready    = "Ready"
	Drifted  = "Drifted"
	Degraded = "Degraded"
)

// ManagedBy labels the ConfigMaps and Secrets the operator writes; its cache holds only those.
const ManagedBy = "app.kubernetes.io/managed-by"

// ManagedByValue is the value of ManagedBy.
const ManagedByValue = "logto-operator"

// DefaultResync is how often an unchanged document is reconciled.
const DefaultResync = 10 * time.Minute

// EndpointIndex indexes LogtoConfigs by the endpoint they manage.
const EndpointIndex = "spec.endpoint"

// EndpointOf is the EndpointIndex value of a LogtoConfig.
func EndpointOf(o client.Object) []string {
	cfg, ok := o.(*v1alpha1.LogtoConfig)
	if !ok {
		return nil
	}
	return []string{strings.TrimRight(cfg.Spec.Endpoint, "/")}
}

// Reconciler reconciles one LogtoConfig at a time (MaxConcurrentReconciles 1): two runs never meet on one Logto.
type Reconciler struct {
	Client client.Client
	// Secrets reads the Secrets a document names, uncached (the manager's API reader): the cache holds only the
	// outputs this operator writes (ManagedBy), not every Secret of the cluster. Client when nil.
	Secrets client.Reader
	Scheme  *runtime.Scheme
	// Now is the clock (tests); time.Now when nil.
	Now func() time.Time
}

// +kubebuilder:rbac:groups=logto.aliud.fr,resources=logtoconfigs,verbs=get;list;watch
// +kubebuilder:rbac:groups=logto.aliud.fr,resources=logtoconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch

// Reconcile runs the document once and writes what happened to its status.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var obj v1alpha1.LogtoConfig
	if err := r.Client.Get(ctx, req.NamespacedName, &obj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	resync := DefaultResync
	if obj.Spec.ResyncInterval != nil && obj.Spec.ResyncInterval.Duration > 0 {
		resync = obj.Spec.ResyncInterval.Duration
	}
	next := ctrl.Result{RequeueAfter: resync}

	out := r.run(ctx, &obj)
	if err := r.writeStatus(ctx, &obj, out); err != nil {
		return ctrl.Result{}, err
	}
	if out.transient != nil {
		// controller-runtime retries with backoff; the resync comes back after.
		return ctrl.Result{}, out.transient
	}
	return next, nil
}

// outcome is what one run learnt, for the status.
type outcome struct {
	degraded  error // the document or the target cannot run until it changes
	transient error // a retry may succeed
	failed    error // an apply op failed for a reason a retry does not fix
	drift     string
	version   string
	applied   bool
}

func (r *Reconciler) run(ctx context.Context, obj *v1alpha1.LogtoConfig) outcome {
	if other, err := r.owner(ctx, obj); err != nil {
		return outcome{transient: err}
	} else if other != "" {
		return outcome{degraded: fmt.Errorf("the endpoint %s is managed by LogtoConfig %s; one LogtoConfig per Logto", obj.Spec.Endpoint, other)}
	}
	cfg, err := config.FromObject(obj)
	if err == nil {
		err = config.Validate(cfg)
	}
	redactor := &secrets.Redactor{}
	redact := func(err error) error { return errors.New(redactor.Redact(err.Error())) }
	if err != nil {
		return outcome{degraded: redact(err)}
	}
	resolver := secrets.Resolver{Redactor: redactor, SecretKey: func(name, key string) (string, error) {
		reader := r.Secrets
		if reader == nil {
			reader = r.Client
		}
		var s corev1.Secret
		if err := reader.Get(ctx, types.NamespacedName{Namespace: obj.Namespace, Name: name}, &s); err != nil {
			return "", err
		}
		return string(s.Data[key]), nil
	}}
	env, err := target.Connect(ctx, &cfg.Spec, resolver)
	if err != nil {
		if engine.Transient(err) {
			return outcome{transient: redact(err)}
		}
		return outcome{degraded: redact(err)}
	}
	env.AllowUntested = cfg.Spec.AllowUntestedLogto
	e, err := engine.New(kinds.All()...)
	if err != nil {
		return outcome{degraded: err}
	}
	out := outcome{version: env.Capability.Version}
	var p *plan.Plan
	if cfg.Spec.Mode == v1alpha1.ModeApply {
		p, err = e.Apply(ctx, env)
		out.applied = err == nil && p != nil && p.Changed()
	} else {
		p, err = e.Plan(ctx, env)
		if err == nil && p.Changed() {
			out.drift = summary(p)
		}
	}
	switch {
	case err == nil:
	case engine.Transient(err):
		out.transient = redact(err)
		return out
	case errors.Is(err, engine.ErrUntested):
		out.degraded = redact(fmt.Errorf("%w (spec.allowUntestedLogto)", err))
		return out
	default:
		var opErr *engine.OpError
		if errors.As(err, &opErr) {
			out.failed = redact(err)
		} else {
			out.degraded = redact(err)
		}
		return out
	}
	if err := r.publish(ctx, obj, env.Outputs, cfg.Spec.Mode == v1alpha1.ModeApply); err != nil {
		out.transient = redact(err)
	}
	return out
}

// owner is the LogtoConfig that manages obj's endpoint when it is not obj: the oldest one (then the first by
// namespace/name).
func (r *Reconciler) owner(ctx context.Context, obj *v1alpha1.LogtoConfig) (string, error) {
	var list v1alpha1.LogtoConfigList
	if err := r.Client.List(ctx, &list, client.MatchingFields{EndpointIndex: EndpointOf(obj)[0]}); err != nil {
		return "", err
	}
	items := list.Items
	sort.Slice(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if !a.CreationTimestamp.Equal(&b.CreationTimestamp) {
			return a.CreationTimestamp.Before(&b.CreationTimestamp)
		}
		return a.Namespace+"/"+a.Name < b.Namespace+"/"+b.Name
	})
	if len(items) == 0 || (items[0].Namespace == obj.Namespace && items[0].Name == obj.Name) {
		return "", nil
	}
	return items[0].Namespace + "/" + items[0].Name, nil
}

// summary is the drift of a plan in one line: how many objects to create, update and delete.
func summary(p *plan.Plan) string {
	return fmt.Sprintf("%d to create, %d to update, %d to delete", p.Count(plan.Create), p.Count(plan.Update), p.Count(plan.Delete))
}

// publish writes the ids to the outputs ConfigMap and, after an apply, the secrets to the outputs Secret.
func (r *Reconciler) publish(ctx context.Context, obj *v1alpha1.LogtoConfig, o *engine.Outputs, applied bool) error {
	if obj.Spec.Outputs == nil || o == nil {
		return nil
	}
	if t := obj.Spec.Outputs.ConfigMap; t != nil && len(o.IDs) > 0 {
		cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: obj.Namespace, Name: t.Name}}
		if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, cm, func() error {
			cm.Labels = merged(cm.Labels)
			cm.Data = map[string]string{}
			for k, name := range published(t, o.IDs) {
				cm.Data[k] = o.IDs[name]
			}
			return controllerutil.SetControllerReference(obj, cm, r.Scheme)
		}); err != nil {
			return fmt.Errorf("outputs ConfigMap %s: %w", t.Name, err)
		}
	}
	if t := obj.Spec.Outputs.Secret; t != nil && applied && len(o.Secrets) > 0 {
		s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: obj.Namespace, Name: t.Name}}
		if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, s, func() error {
			s.Labels = merged(s.Labels)
			s.Data = map[string][]byte{}
			for k, name := range published(t, o.Secrets) {
				s.Data[k] = []byte(o.Secrets[name].Reveal())
			}
			return controllerutil.SetControllerReference(obj, s, r.Scheme)
		}); err != nil {
			return fmt.Errorf("outputs Secret %s: %w", t.Name, err)
		}
	}
	return nil
}

// published maps each key a target publishes to the output it holds: the target's keys (those already known), or
// every output under its own name.
func published[V any](t *v1alpha1.OutputTarget, outputs map[string]V) map[string]string {
	out := map[string]string{}
	if len(t.Keys) == 0 {
		for name := range outputs {
			out[name] = name
		}
		return out
	}
	for key, name := range t.Keys {
		if _, ok := outputs[name]; ok {
			out[key] = name
		}
	}
	return out
}

func merged(labels map[string]string) map[string]string {
	if labels == nil {
		labels = map[string]string{}
	}
	labels[ManagedBy] = ManagedByValue
	return labels
}

func (r *Reconciler) writeStatus(ctx context.Context, obj *v1alpha1.LogtoConfig, out outcome) error {
	now := time.Now
	if r.Now != nil {
		now = r.Now
	}
	gen := obj.Generation
	set := func(typ string, status metav1.ConditionStatus, reason, msg string) {
		apimeta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
			Type: typ, Status: status, Reason: reason, Message: msg, ObservedGeneration: gen,
		})
	}
	switch {
	case out.degraded != nil:
		set(Degraded, metav1.ConditionTrue, "InvalidOrUnreachable", out.degraded.Error())
		set(Ready, metav1.ConditionFalse, "Degraded", "see the Degraded condition")
	case out.failed != nil:
		set(Degraded, metav1.ConditionTrue, "ApplyFailed", out.failed.Error())
		set(Ready, metav1.ConditionFalse, "ApplyFailed", "see the Degraded condition")
	case out.transient != nil:
		set(Degraded, metav1.ConditionFalse, "Transient", "")
		set(Ready, metav1.ConditionFalse, "Transient", out.transient.Error())
	default:
		set(Degraded, metav1.ConditionFalse, "Reconciled", "")
		if out.drift != "" {
			set(Drifted, metav1.ConditionTrue, "Observed", "Logto differs from the document: "+out.drift)
			set(Ready, metav1.ConditionFalse, "Drifted", "Observe mode: "+out.drift+" (set spec.mode to Apply to reconcile)")
		} else {
			set(Drifted, metav1.ConditionFalse, "InSync", "")
			set(Ready, metav1.ConditionTrue, "InSync", "Logto matches the document")
		}
		obj.Status.ObservedGeneration = gen
	}
	if out.version != "" {
		obj.Status.LogtoVersion = out.version
	}
	if out.applied {
		t := metav1.NewTime(now())
		obj.Status.LastAppliedTime = &t
	}
	return r.Client.Status().Update(ctx, obj)
}

// SetupWithManager registers the controller: LogtoConfigs and the outputs they own. A changed credential Secret is
// read at the next resync (the cache does not hold it).
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &v1alpha1.LogtoConfig{}, EndpointIndex, EndpointOf); err != nil {
		return err
	}
	if r.Secrets == nil {
		r.Secrets = mgr.GetAPIReader()
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.LogtoConfig{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&corev1.Secret{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: 1}).
		Complete(r)
}
