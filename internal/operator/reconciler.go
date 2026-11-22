// Package operator reconciles LogtoConfig objects with the engine of logtoctl: one plan (Observe) or one apply
// (Apply) per reconcile, its outcome written as conditions, the generated ids and secrets published in a ConfigMap
// and a Secret of the document's namespace.
package operator

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

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

// ReadableByAnnotation lists, comma-separated, the Logto endpoints a Secret may be sent to: a LogtoConfig of its
// namespace reads it (secretKeyRef) only when its endpoint, and its token endpoint when it has one, are listed
// (compared like the endpoint index). Without it, whoever may create a LogtoConfig could send the Secret, the Logto
// credentials included, to an endpoint of their choice.
const ReadableByAnnotation = "logto.aliud.fr/readable-by"

// DefaultResync is how often an unchanged document is reconciled.
const DefaultResync = 10 * time.Minute

// EndpointIndex indexes LogtoConfigs by the endpoint they manage.
const EndpointIndex = "spec.endpoint"

// EndpointOf is the EndpointIndex value of a LogtoConfig: its endpoint without what does not change the Logto it
// reaches (scheme, case of the host, default port, trailing slashes). One Logto reached through two hosts (a
// Service and an ingress) cannot be told apart: name it one way.
func EndpointOf(o client.Object) []string {
	cfg, ok := o.(*v1alpha1.LogtoConfig)
	if !ok {
		return nil
	}
	return []string{normalized(cfg.Spec.Endpoint)}
}

func normalized(endpoint string) string {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || u.Host == "" {
		return strings.ToLower(strings.TrimRight(endpoint, "/"))
	}
	host, port := strings.ToLower(u.Hostname()), u.Port()
	scheme := strings.ToLower(u.Scheme)
	if (scheme == "https" && port == "443") || (scheme == "http" && port == "80") {
		port = ""
	}
	if port != "" {
		host += ":" + port
	}
	return host + strings.TrimRight(u.EscapedPath(), "/")
}

// Reconciler reconciles LogtoConfigs: several at a time (Concurrency), never two on one endpoint.
type Reconciler struct {
	Client client.Client
	// Secrets reads the Secrets a document names, uncached (the manager's API reader): the cache holds only the
	// outputs this operator writes (ManagedBy), not every Secret of the cluster. Client when nil.
	Secrets client.Reader
	Scheme  *runtime.Scheme
	// Now is the clock (tests); time.Now when nil.
	Now func() time.Time
	// Concurrency is how many LogtoConfigs reconcile at once (default 4); runs on one endpoint take turns.
	Concurrency int

	locksMu sync.Mutex
	locks   map[string]*endpointLock // normalized endpoint, while a run holds or waits for it
}

type endpointLock struct {
	turn  chan struct{} // holds one token while a run has the endpoint
	users int
}

// lock waits for the endpoint's turn, or for ctx, and returns its release.
func (r *Reconciler) lock(ctx context.Context, endpoint string) (func(), error) {
	key := normalized(endpoint)
	r.locksMu.Lock()
	if r.locks == nil {
		r.locks = map[string]*endpointLock{}
	}
	l := r.locks[key]
	if l == nil {
		l = &endpointLock{turn: make(chan struct{}, 1)}
		r.locks[key] = l
	}
	l.users++
	r.locksMu.Unlock()
	leave := func() {
		r.locksMu.Lock()
		if l.users--; l.users == 0 {
			delete(r.locks, key)
		}
		r.locksMu.Unlock()
	}
	select {
	case l.turn <- struct{}{}:
		return func() { <-l.turn; leave() }, nil
	case <-ctx.Done():
		leave()
		return nil, ctx.Err()
	}
}

// +kubebuilder:rbac:groups=logto.aliud.fr,resources=logtoconfigs,verbs=get;list;watch
// +kubebuilder:rbac:groups=logto.aliud.fr,resources=logtoconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=logto.aliud.fr,resources=logtoconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch

// Reconcile runs the document once and writes what happened to its status.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var obj v1alpha1.LogtoConfig
	if err := r.Client.Get(ctx, req.NamespacedName, &obj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !obj.DeletionTimestamp.IsZero() {
		// Being deleted: no write to Logto, no output re-created under the garbage collector.
		return ctrl.Result{}, nil
	}
	resync := DefaultResync
	if obj.Spec.ResyncInterval != nil && obj.Spec.ResyncInterval.Duration > 0 {
		resync = obj.Spec.ResyncInterval.Duration
	}
	next := ctrl.Result{RequeueAfter: resync}

	// Whether this object has the endpoint is known without its turn: a waiting worker is one another endpoint lacks.
	var out outcome
	switch other, err := r.owner(ctx, &obj); {
	case err != nil:
		out = outcome{transient: err}
	case other != "":
		out = outcome{reason: "EndpointTaken", degraded: fmt.Errorf("the endpoint %s is managed by LogtoConfig %s; one LogtoConfig per Logto", obj.Spec.Endpoint, other)}
	default:
		release, err := r.lock(ctx, obj.Spec.Endpoint)
		if err != nil {
			out = outcome{transient: err}
			break
		}
		out = r.run(ctx, &obj)
		release()
	}
	if err := r.writeStatus(ctx, req.NamespacedName, obj.Generation, out); err != nil {
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
	degraded  error  // the document or the target cannot run until it changes
	transient error  // a retry may succeed
	failed    error  // an apply op failed for a reason a retry does not fix
	reason    string // of Degraded
	drift     string
	connected bool // the target answered: version is what it matched (empty when untested)
	version   string
	applied   bool      // an apply wrote to Logto, even if it then failed
	appliedAt time.Time // when that apply ended
}

// secretError is a Secret a document names that could not be read.
type secretError struct {
	err       error
	transient bool // the API server may answer later
}

func (e *secretError) Error() string { return e.err.Error() }
func (e *secretError) Unwrap() error { return e.err }

func (r *Reconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Reconciler) reader() client.Reader {
	if r.Secrets != nil {
		return r.Secrets
	}
	return r.Client
}

// readableBy are the normalized endpoints a Secret's annotation lists.
func readableBy(s *corev1.Secret) map[string]bool {
	out := map[string]bool{}
	for _, e := range strings.Split(s.Annotations[ReadableByAnnotation], ",") {
		if e = strings.TrimSpace(e); e != "" {
			out[normalized(e)] = true
		}
	}
	return out
}

func (r *Reconciler) run(ctx context.Context, obj *v1alpha1.LogtoConfig) outcome {
	cfg, err := config.FromObject(obj)
	if err == nil {
		err = config.Validate(cfg)
	}
	redactor := &secrets.Redactor{}
	redact := func(err error) error { return errors.New(redactor.Redact(err.Error())) }
	if err != nil {
		return outcome{reason: "InvalidDocument", degraded: redact(err)}
	}
	// The operator never reads its own environment or files: a document could send them to its endpoint.
	for _, src := range config.Sources(&cfg.Spec) {
		if src.From.SecretKeyRef == nil {
			return outcome{reason: "InvalidDocument", degraded: fmt.Errorf("%s: a LogtoConfig object reads secretKeyRef only (env and file are logtoctl's)", src.Path)}
		}
	}
	// Where a Secret's value goes: the endpoint, and the token endpoint when it has its own.
	targets := []string{normalized(cfg.Spec.Endpoint)}
	if te := cfg.Spec.Credentials.TokenEndpoint; te != "" {
		targets = append(targets, normalized(te))
	}
	resolver := secrets.Resolver{Redactor: redactor, SecretKey: func(name, key string) (string, error) {
		var s corev1.Secret
		if err := r.reader().Get(ctx, types.NamespacedName{Namespace: obj.Namespace, Name: name}, &s); err != nil {
			return "", &secretError{err: err, transient: transientAPI(err)}
		}
		allowed := readableBy(&s)
		var refused []string
		for _, t := range targets {
			if !allowed[t] {
				refused = append(refused, t)
			}
		}
		if len(refused) > 0 {
			return "", &secretError{err: fmt.Errorf("the Secret %s does not allow LogtoConfig objects to send it to %s (annotation %s)",
				name, strings.Join(refused, ", "), ReadableByAnnotation)}
		}
		return string(s.Data[key]), nil
	}}
	failed := func(err error, reason string) outcome {
		var se *secretError
		switch {
		case errors.As(err, &se) && se.transient, engine.Transient(err):
			return outcome{transient: redact(err)}
		case errors.As(err, &se):
			return outcome{reason: "SecretNotReadable", degraded: redact(err)}
		}
		return outcome{reason: reason, degraded: redact(err)}
	}
	// Every Secret is read before the first call: one that cannot be read stops the run before any write.
	for _, src := range config.Sources(&cfg.Spec) {
		if _, err := resolver.Secret(strings.TrimSuffix(src.Path, ".valueFrom"), v1alpha1.SecretSource{ValueFrom: src.From}); err != nil {
			return failed(err, "SecretNotReadable")
		}
	}
	env, err := target.Connect(ctx, &cfg.Spec, resolver)
	if err != nil {
		return failed(err, "TargetRefused")
	}
	env.AllowUntested = cfg.Spec.AllowUntestedLogto
	e, err := engine.New(kinds.All()...)
	if err != nil {
		return outcome{degraded: err}
	}
	out := outcome{connected: true, version: env.Capability.Version}
	var p *plan.Plan
	if cfg.Spec.Mode == v1alpha1.ModeApply {
		p, err = e.Apply(ctx, env)
		out.applied, out.appliedAt = wrote(p), r.now()
	} else {
		p, err = e.Plan(ctx, env)
		if err == nil && p.Changed() {
			out.drift = summary(p)
		}
	}
	var se *secretError
	switch {
	case err == nil:
	case errors.As(err, &se):
		f := failed(err, "SecretNotReadable")
		out.transient, out.reason, out.degraded = f.transient, f.reason, f.degraded
		return out
	case engine.Transient(err):
		out.transient = redact(err)
		return out
	case errors.Is(err, engine.ErrUntested):
		out.reason, out.degraded = "UntestedLogto", redact(fmt.Errorf("%w (spec.allowUntestedLogto)", err))
		return out
	default:
		var opErr *engine.OpError
		if errors.As(err, &opErr) {
			out.failed = redact(err)
		} else {
			out.reason, out.degraded = "ReconcileFailed", redact(err)
		}
		return out
	}
	if err := r.publish(ctx, obj, env.Outputs, cfg.Spec.Mode == v1alpha1.ModeApply); err != nil {
		if errors.Is(err, errOutputsTaken) || apierrors.IsInvalid(err) {
			out.reason, out.degraded = "OutputsConflict", redact(err)
		} else {
			out.transient = redact(err)
		}
	}
	return out
}

// wrote reports whether an apply wrote anything to Logto (an op applied), whatever came after.
func wrote(p *plan.Plan) bool {
	if p == nil {
		return false
	}
	for _, op := range p.Ops {
		if op.Result == plan.Applied {
			return true
		}
	}
	return false
}

// transientAPI reports whether the API server may answer a request later.
func transientAPI(err error) bool {
	return apierrors.IsServerTimeout(err) || apierrors.IsTimeout(err) || apierrors.IsTooManyRequests(err) ||
		apierrors.IsServiceUnavailable(err) || apierrors.IsInternalError(err) || apierrors.IsUnexpectedServerError(err) ||
		engine.Transient(err)
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

// Siblings are the other LogtoConfigs of obj's endpoint: when obj goes away or moves, one of them takes the
// endpoint over.
func (r *Reconciler) Siblings(ctx context.Context, obj client.Object) []reconcile.Request {
	keys := EndpointOf(obj)
	if len(keys) == 0 {
		return nil
	}
	var list v1alpha1.LogtoConfigList
	if err := r.Client.List(ctx, &list, client.MatchingFields{EndpointIndex: keys[0]}); err != nil {
		return nil
	}
	var out []reconcile.Request
	for _, cfg := range list.Items {
		if cfg.Namespace != obj.GetNamespace() || cfg.Name != obj.GetName() {
			out = append(out, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: cfg.Namespace, Name: cfg.Name}})
		}
	}
	return out
}

// summary is the drift of a plan in one line: how many objects to create, update and delete.
func summary(p *plan.Plan) string {
	return fmt.Sprintf("%d to create, %d to update, %d to delete", p.Count(plan.Create), p.Count(plan.Update), p.Count(plan.Delete))
}

// errOutputsTaken is an outputs name held by an object this LogtoConfig does not own.
var errOutputsTaken = errors.New("exists and is not managed by this LogtoConfig: choose another name, or delete it")

// publish writes the ids to the outputs ConfigMap and, after an apply, the secrets to the outputs Secret: each
// holds exactly what the document publishes now.
func (r *Reconciler) publish(ctx context.Context, obj *v1alpha1.LogtoConfig, o *engine.Outputs, applied bool) error {
	if obj.Spec.Outputs == nil || o == nil {
		return nil
	}
	if t := obj.Spec.Outputs.ConfigMap; t != nil {
		cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: obj.Namespace, Name: t.Name}}
		if err := r.own(ctx, obj, cm, func(x client.Object) {
			c := x.(*corev1.ConfigMap)
			c.Data = map[string]string{}
			for k, name := range published(t, o.IDs) {
				c.Data[k] = o.IDs[name]
			}
		}); err != nil {
			return fmt.Errorf("outputs ConfigMap %s: %w", t.Name, err)
		}
	}
	if t := obj.Spec.Outputs.Secret; t != nil && applied {
		s := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: obj.Namespace, Name: t.Name}}
		if err := r.own(ctx, obj, s, func(x client.Object) {
			sec := x.(*corev1.Secret)
			sec.Data = map[string][]byte{}
			for k, name := range published(t, o.Secrets) {
				sec.Data[k] = []byte(o.Secrets[name].Reveal())
			}
		}); err != nil {
			return fmt.Errorf("outputs Secret %s: %w", t.Name, err)
		}
	}
	return nil
}

// own creates or updates an output object that obj controls, and never touches one it does not. The cache holds
// only labelled objects, so an existing one it misses (a foreign object, ours before the cache saw it, ours with its
// label removed) shows up as AlreadyExists at create: it is read again from the API server, and taken back only if
// obj controls it.
func (r *Reconciler) own(ctx context.Context, obj *v1alpha1.LogtoConfig, o client.Object, fill func(client.Object)) error {
	mutate := func(x client.Object) error {
		if x.GetResourceVersion() != "" && !metav1.IsControlledBy(x, obj) {
			return errOutputsTaken
		}
		x.SetLabels(merged(x.GetLabels()))
		fill(x)
		return controllerutil.SetControllerReference(obj, x, r.Scheme)
	}
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, o, func() error { return mutate(o) })
	if !apierrors.IsAlreadyExists(err) {
		return err
	}
	fresh, ok := o.DeepCopyObject().(client.Object)
	if !ok {
		return errOutputsTaken
	}
	if err := r.reader().Get(ctx, client.ObjectKeyFromObject(o), fresh); err != nil {
		return err
	}
	if err := mutate(fresh); err != nil {
		return err
	}
	return r.Client.Update(ctx, fresh)
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

// writeStatus writes the outcome of a run on the newest version of the object, retried on conflict: someone may
// have written it while the run talked to Logto. gen is the generation the run read.
func (r *Reconciler) writeStatus(ctx context.Context, key types.NamespacedName, gen int64, out outcome) error {
	appliedAt := metav1.NewTime(out.appliedAt)
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var obj v1alpha1.LogtoConfig
		if err := r.Client.Get(ctx, key, &obj); err != nil {
			return client.IgnoreNotFound(err)
		}
		setStatus(&obj.Status, gen, out, appliedAt)
		return r.Client.Status().Update(ctx, &obj)
	})
}

func setStatus(st *v1alpha1.LogtoConfigStatus, gen int64, out outcome, appliedAt metav1.Time) {
	set := func(typ string, status metav1.ConditionStatus, reason, msg string) {
		apimeta.SetStatusCondition(&st.Conditions, metav1.Condition{
			Type: typ, Status: status, Reason: reason, Message: msg, ObservedGeneration: gen,
		})
	}
	reason := out.reason
	if reason == "" {
		reason = "Error"
	}
	switch {
	case out.degraded != nil:
		set(Degraded, metav1.ConditionTrue, reason, out.degraded.Error())
		set(Ready, metav1.ConditionFalse, "Degraded", "see the Degraded condition")
		set(Drifted, metav1.ConditionUnknown, "NotPlanned", "the last run did not plan")
	case out.failed != nil:
		set(Degraded, metav1.ConditionTrue, "ApplyFailed", out.failed.Error())
		set(Ready, metav1.ConditionFalse, "ApplyFailed", "see the Degraded condition")
		set(Drifted, metav1.ConditionUnknown, "NotPlanned", "the last apply stopped before its end")
	case out.transient != nil:
		// A transient failure says nothing of the document: Degraded keeps what the last full run found.
		set(Ready, metav1.ConditionFalse, "Transient", out.transient.Error())
		set(Drifted, metav1.ConditionUnknown, "NotPlanned", "the last run did not finish")
	default:
		set(Degraded, metav1.ConditionFalse, "Reconciled", "")
		if out.drift != "" {
			set(Drifted, metav1.ConditionTrue, "Observed", "Logto differs from the document: "+out.drift)
			set(Ready, metav1.ConditionFalse, "Drifted", "Observe mode: "+out.drift+" (set spec.mode to Apply to reconcile)")
		} else {
			set(Drifted, metav1.ConditionFalse, "InSync", "")
			set(Ready, metav1.ConditionTrue, "InSync", "Logto matches the document")
		}
	}
	// The run read this generation, whatever came of it: Ready says whether it succeeded.
	st.ObservedGeneration = gen
	if out.connected {
		st.LogtoVersion = out.version // empty for an untested Logto
	}
	if out.applied {
		st.LastAppliedTime = &appliedAt
	}
}

// SetupWithManager registers the controller. A LogtoConfig reconciles when its spec, labels or annotations change
// (not its status, which every run writes), when an output it owns changes, when another LogtoConfig of its
// endpoint goes away or moves, and every resync. A changed credential Secret is read at the next resync (the cache
// does not hold it).
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &v1alpha1.LogtoConfig{}, EndpointIndex, EndpointOf); err != nil {
		return err
	}
	if r.Secrets == nil {
		r.Secrets = mgr.GetAPIReader()
	}
	concurrency := r.Concurrency
	if concurrency < 1 {
		concurrency = 4
	}
	changed := predicate.Or(predicate.GenerationChangedPredicate{}, predicate.LabelChangedPredicate{}, predicate.AnnotationChangedPredicate{})
	// An output appears because a run created it, and a new LogtoConfig never takes an endpoint over: neither
	// creation needs another run.
	notCreated := predicate.Funcs{CreateFunc: func(event.CreateEvent) bool { return false }}
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.LogtoConfig{}, builder.WithPredicates(changed)).
		Owns(&corev1.ConfigMap{}, builder.WithPredicates(notCreated)).
		Owns(&corev1.Secret{}, builder.WithPredicates(notCreated)).
		Watches(&v1alpha1.LogtoConfig{}, handler.EnqueueRequestsFromMapFunc(r.Siblings),
			builder.WithPredicates(predicate.GenerationChangedPredicate{}, notCreated)).
		WithOptions(controller.Options{MaxConcurrentReconciles: concurrency}).
		Complete(r)
}
