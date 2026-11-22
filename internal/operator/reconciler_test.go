package operator_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/yaml"

	"github.com/Aliud-Corp/logto-operator/api/v1alpha1"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindtest"
	"github.com/Aliud-Corp/logto-operator/internal/operator"
)

const ns = "tenant"

// doc is a LogtoConfig on the fake Logto: a resource and an M2M application whose id and secret are outputs.
func doc(t *testing.T, endpoint, name, mode string) *v1alpha1.LogtoConfig {
	t.Helper()
	body := `apiVersion: logto.aliud.fr/v1alpha1
kind: LogtoConfig
metadata: {name: ` + name + `, namespace: ` + ns + `, generation: 2}
spec:
  endpoint: ` + endpoint + `
  mode: ` + mode + `
  credentials:
    clientId: {valueFrom: {secretKeyRef: {name: logto-credentials, key: id}}}
    clientSecret: {valueFrom: {secretKeyRef: {name: logto-credentials, key: secret}}}
  outputs:
    configMap: {name: logto-ids}
    secret: {name: logto-secrets}
  resources:
    app: {name: Example App API, indicator: https://app.example.test/api, scopes: {"event:read": {}}}
  applications:
    backend:
      name: Example Backend
      type: MachineToMachine
      roles: [{name: Logto Management API access}]
      secret: {publish: true}
`
	var cfg v1alpha1.LogtoConfig
	if err := yaml.UnmarshalStrict([]byte(body), &cfg); err != nil {
		t.Fatal(err)
	}
	return &cfg
}

func credentials() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "logto-credentials", Namespace: ns, Annotations: map[string]string{operator.ReadableAnnotation: "true"}},
		Data:       map[string][]byte{"id": []byte("boot"), "secret": []byte("boot-secret")},
	}
}

func setup(t *testing.T, objs ...client.Object) (*operator.Reconciler, client.Client) {
	t.Helper()
	return setupWith(t, interceptor.Funcs{}, objs...)
}

func setupWith(t *testing.T, funcs interceptor.Funcs, objs ...client.Object) (*operator.Reconciler, client.Client) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).
		WithStatusSubresource(&v1alpha1.LogtoConfig{}).
		WithIndex(&v1alpha1.LogtoConfig{}, operator.EndpointIndex, operator.EndpointOf).
		WithInterceptorFuncs(funcs).
		Build()
	return &operator.Reconciler{Client: c, Scheme: scheme}, c
}

func reconcile(t *testing.T, r *operator.Reconciler, name string) (ctrl.Result, error) {
	t.Helper()
	return r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: name}})
}

func get(t *testing.T, c client.Client, name string) *v1alpha1.LogtoConfig {
	t.Helper()
	var cfg v1alpha1.LogtoConfig
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: name}, &cfg); err != nil {
		t.Fatal(err)
	}
	return &cfg
}

func condition(t *testing.T, cfg *v1alpha1.LogtoConfig, typ string) metav1.Condition {
	t.Helper()
	c := apimeta.FindStatusCondition(cfg.Status.Conditions, typ)
	if c == nil {
		t.Fatalf("no %s condition: %+v", typ, cfg.Status.Conditions)
	}
	return *c
}

// Apply reconciles Logto, publishes the ids in a ConfigMap and the secrets in a Secret (both owned by the
// LogtoConfig), reports Ready, and comes back after the resync interval.
func TestApplyReconcilesLogtoAndPublishesTheOutputs(t *testing.T) {
	fakeLogto, lc := kindtest.Fake(t)
	r, c := setup(t, doc(t, lc.Endpoint(), "main", v1alpha1.ModeApply), credentials())

	res, err := reconcile(t, r, "main")
	if err != nil {
		t.Fatal(err)
	}
	if res.RequeueAfter != 10*time.Minute {
		t.Fatalf("requeue after %v, want the 10m default", res.RequeueAfter)
	}
	cfg := get(t, c, "main")
	if ready := condition(t, cfg, operator.Ready); ready.Status != metav1.ConditionTrue || ready.ObservedGeneration != 2 {
		t.Fatalf("Ready = %+v", ready)
	}
	if cfg.Status.ObservedGeneration != 2 || cfg.Status.LogtoVersion == "" || cfg.Status.LastAppliedTime == nil {
		t.Fatalf("status = %+v", cfg.Status)
	}
	var backendID string
	for _, a := range fakeLogto.Applications() {
		if a["name"] == "Example Backend" {
			backendID = a["id"].(string)
		}
	}
	var ids corev1.ConfigMap
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "logto-ids"}, &ids); err != nil {
		t.Fatal(err)
	}
	if ids.Data["applications.backend.id"] != backendID || ids.Data["resources.app.indicator"] != "https://app.example.test/api" {
		t.Fatalf("ids = %v", ids.Data)
	}
	var sec corev1.Secret
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "logto-secrets"}, &sec); err != nil {
		t.Fatal(err)
	}
	if string(sec.Data["applications.backend.secret"]) != fakeLogto.ApplicationSecrets(backendID)[0] {
		t.Fatalf("secret keys = %v", keys(sec.Data))
	}
	for _, o := range []metav1.Object{&ids, &sec} {
		if refs := o.GetOwnerReferences(); len(refs) != 1 || refs[0].Name != "main" || refs[0].Kind != v1alpha1.Kind {
			t.Fatalf("%s owners = %+v", o.GetName(), refs)
		}
		// The operator's cache holds only what carries this label.
		if o.GetLabels()[operator.ManagedBy] != operator.ManagedByValue {
			t.Fatalf("%s labels = %v", o.GetName(), o.GetLabels())
		}
	}

	writes := fakeLogto.Writes()
	if _, err := reconcile(t, r, "main"); err != nil {
		t.Fatal(err)
	}
	if fakeLogto.Writes() != writes {
		t.Fatalf("an unchanged document wrote %d times", fakeLogto.Writes()-writes)
	}
}

// Observe (the default) writes nothing to Logto and reports the drift.
func TestObserveWritesNothingAndReportsDrift(t *testing.T) {
	fakeLogto, lc := kindtest.Fake(t)
	r, c := setup(t, doc(t, lc.Endpoint(), "main", v1alpha1.ModeObserve), credentials())
	if _, err := reconcile(t, r, "main"); err != nil {
		t.Fatal(err)
	}
	if fakeLogto.Writes() != 0 {
		t.Fatalf("Observe wrote %d times", fakeLogto.Writes())
	}
	cfg := get(t, c, "main")
	if d := condition(t, cfg, operator.Drifted); d.Status != metav1.ConditionTrue || !strings.Contains(d.Message, "to create") {
		t.Fatalf("Drifted = %+v", d)
	}
	if ready := condition(t, cfg, operator.Ready); ready.Status != metav1.ConditionFalse {
		t.Fatalf("Ready = %+v", ready)
	}
	var sec corev1.Secret
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "logto-secrets"}, &sec); err == nil {
		t.Fatal("Observe published secrets")
	}
}

// A document that cannot run (a missing Secret, a file to read) is Degraded, without a write, until it changes.
func TestADocumentInErrorIsDegraded(t *testing.T) {
	for name, tc := range map[string]struct {
		mutate func(*v1alpha1.LogtoConfig)
		objs   []client.Object
		want   string
	}{
		"missing credentials": {func(*v1alpha1.LogtoConfig) {}, nil, `secrets "logto-credentials" not found`},
		// The operator never reads its own environment or files: a document could send them to its endpoint.
		"an env source": {func(cfg *v1alpha1.LogtoConfig) {
			cfg.Spec.Credentials.ClientID = v1alpha1.StringSource{ValueFrom: &v1alpha1.ValueFrom{Env: "HOME"}}
		}, []client.Object{credentials()}, "spec.credentials.clientId.valueFrom: a LogtoConfig object reads secretKeyRef only"},
		"a file source": {func(cfg *v1alpha1.LogtoConfig) {
			cfg.Spec.Credentials.ClientSecret = v1alpha1.SecretSource{ValueFrom: v1alpha1.ValueFrom{File: "/var/run/secrets/kubernetes.io/serviceaccount/token"}}
		}, []client.Object{credentials()}, "spec.credentials.clientSecret.valueFrom: a LogtoConfig object reads secretKeyRef only"},
		// A Secret is read only when it says so: otherwise whoever may create a LogtoConfig could send any Secret of
		// the namespace to an endpoint of their choice.
		"a Secret that does not opt in": {func(*v1alpha1.LogtoConfig) {}, []client.Object{&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "logto-credentials", Namespace: ns},
			Data:       map[string][]byte{"id": []byte("boot"), "secret": []byte("boot-secret")},
		}}, `the Secret logto-credentials does not allow LogtoConfig objects to read it (annotation logto.aliud.fr/readable: "true")`},
		"a file to read": {func(cfg *v1alpha1.LogtoConfig) {
			cfg.Spec.SignInExperience = &v1alpha1.SignInExperience{CustomCSS: &v1alpha1.TextSource{FromFile: "theme.css"}}
		}, []client.Object{credentials()}, "logtoctl render"},
	} {
		t.Run(name, func(t *testing.T) {
			fakeLogto, lc := kindtest.Fake(t)
			cfg := doc(t, lc.Endpoint(), "main", v1alpha1.ModeApply)
			tc.mutate(cfg)
			r, c := setup(t, append(tc.objs, cfg)...)
			res, err := reconcile(t, r, "main")
			if err != nil {
				t.Fatalf("a document in error is not retried with backoff: %v", err)
			}
			if res.RequeueAfter == 0 {
				t.Fatal("a degraded document must still be resynced")
			}
			got := get(t, c, "main")
			if d := condition(t, got, operator.Degraded); d.Status != metav1.ConditionTrue || !strings.Contains(d.Message, tc.want) {
				t.Fatalf("Degraded = %+v", d)
			}
			if fakeLogto.Writes() != 0 {
				t.Fatalf("wrote %d times", fakeLogto.Writes())
			}
		})
	}
}

// A transient failure (Logto answering 503) is retried with backoff: the error goes back to controller-runtime.
func TestATransientFailureIsRetried(t *testing.T) {
	fakeLogto, lc := kindtest.Fake(t)
	fakeLogto.FailNext("POST /api/resources", 503)
	r, c := setup(t, doc(t, lc.Endpoint(), "main", v1alpha1.ModeApply), credentials())
	if _, err := reconcile(t, r, "main"); err == nil {
		t.Fatal("a transient failure must be returned, for a retry with backoff")
	}
	cfg := get(t, c, "main")
	if ready := condition(t, cfg, operator.Ready); ready.Status != metav1.ConditionFalse || ready.Reason != "Transient" {
		t.Fatalf("Ready = %+v", ready)
	}
	if d := apimeta.FindStatusCondition(cfg.Status.Conditions, operator.Degraded); d != nil && d.Status == metav1.ConditionTrue {
		t.Fatalf("a transient failure is not Degraded: %+v", d)
	}
}

// One LogtoConfig per Logto endpoint: two would fight over the sign-in experience and prune each other's objects.
// The oldest keeps it; the other is Degraded and never writes.
func TestASecondLogtoConfigOnTheSameEndpointIsRefused(t *testing.T) {
	fakeLogto, lc := kindtest.Fake(t)
	first := doc(t, lc.Endpoint(), "first", v1alpha1.ModeObserve)
	first.CreationTimestamp = metav1.NewTime(time.Now().Add(-time.Hour))
	second := doc(t, lc.Endpoint(), "second", v1alpha1.ModeApply)
	second.CreationTimestamp = metav1.NewTime(time.Now())
	r, c := setup(t, first, second, credentials())
	if _, err := reconcile(t, r, "second"); err != nil {
		t.Fatal(err)
	}
	if d := condition(t, get(t, c, "second"), operator.Degraded); d.Status != metav1.ConditionTrue || !strings.Contains(d.Message, "tenant/first") {
		t.Fatalf("Degraded = %+v", d)
	}
	if fakeLogto.Writes() != 0 {
		t.Fatalf("wrote %d times", fakeLogto.Writes())
	}
}

func keys(m map[string][]byte) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// With keys, an output target publishes the outputs it lists, under the names it gives (envFrom-ready), and nothing
// else.
func TestOutputKeysRenameWhatIsPublished(t *testing.T) {
	fakeLogto, lc := kindtest.Fake(t)
	cfg := doc(t, lc.Endpoint(), "main", v1alpha1.ModeApply)
	cfg.Spec.Outputs.ConfigMap.Keys = map[string]string{"LOGTO_MANAGEMENT_CLIENT_ID": "applications.backend.id"}
	cfg.Spec.Outputs.Secret.Keys = map[string]string{"LOGTO_MANAGEMENT_CLIENT_SECRET": "applications.backend.secret"}
	r, c := setup(t, cfg, credentials())
	if _, err := reconcile(t, r, "main"); err != nil {
		t.Fatal(err)
	}
	var backendID string
	for _, a := range fakeLogto.Applications() {
		if a["name"] == "Example Backend" {
			backendID = a["id"].(string)
		}
	}
	var ids corev1.ConfigMap
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "logto-ids"}, &ids); err != nil {
		t.Fatal(err)
	}
	if len(ids.Data) != 1 || ids.Data["LOGTO_MANAGEMENT_CLIENT_ID"] != backendID {
		t.Fatalf("ids = %v", ids.Data)
	}
	var sec corev1.Secret
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "logto-secrets"}, &sec); err != nil {
		t.Fatal(err)
	}
	if len(sec.Data) != 1 || string(sec.Data["LOGTO_MANAGEMENT_CLIENT_SECRET"]) != fakeLogto.ApplicationSecrets(backendID)[0] {
		t.Fatalf("secret keys = %v", keys(sec.Data))
	}
}

// Someone writes the object while a run talks to Logto: the status is written anyway, on the newest object, and
// lastAppliedTime is not lost.
func TestAStatusConflictIsRetriedOnTheNewestObject(t *testing.T) {
	_, lc := kindtest.Fake(t)
	conflicts := 1
	funcs := interceptor.Funcs{SubResourceUpdate: func(ctx context.Context, c client.Client, sub string, obj client.Object, opts ...client.SubResourceUpdateOption) error {
		if conflicts > 0 {
			conflicts--
			return apierrors.NewConflict(schema.GroupResource{Group: "logto.aliud.fr", Resource: "logtoconfigs"}, obj.GetName(), errors.New("changed"))
		}
		return c.SubResource(sub).Update(ctx, obj, opts...)
	}}
	r, c := setupWith(t, funcs, doc(t, lc.Endpoint(), "main", v1alpha1.ModeApply), credentials())
	if _, err := reconcile(t, r, "main"); err != nil {
		t.Fatal(err)
	}
	cfg := get(t, c, "main")
	if cfg.Status.LastAppliedTime == nil || condition(t, cfg, operator.Ready).Status != metav1.ConditionTrue {
		t.Fatalf("status = %+v", cfg.Status)
	}
}

// A run that fails says nothing about drift: Drifted becomes Unknown instead of keeping an old answer.
func TestAFailedRunMakesDriftUnknown(t *testing.T) {
	_, lc := kindtest.Fake(t)
	cfg := doc(t, lc.Endpoint(), "main", v1alpha1.ModeObserve)
	r, c := setup(t, cfg, credentials())
	if _, err := reconcile(t, r, "main"); err != nil {
		t.Fatal(err)
	}
	if d := condition(t, get(t, c, "main"), operator.Drifted); d.Status != metav1.ConditionTrue {
		t.Fatalf("Drifted = %+v", d)
	}
	if err := c.Delete(context.Background(), credentials()); err != nil {
		t.Fatal(err)
	}
	if _, err := reconcile(t, r, "main"); err != nil {
		t.Fatal(err)
	}
	if d := condition(t, get(t, c, "main"), operator.Drifted); d.Status != metav1.ConditionUnknown {
		t.Fatalf("Drifted = %+v", d)
	}
}

// An outputs name that is taken by an object this operator does not manage is never overwritten: the document is
// Degraded until the name changes, instead of retrying for ever.
func TestAnOutputsNameTakenByAnotherObjectIsRefused(t *testing.T) {
	_, lc := kindtest.Fake(t)
	theirs := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "logto-secrets", Namespace: ns}, Data: map[string][]byte{"keep": []byte("me")}}
	r, c := setup(t, doc(t, lc.Endpoint(), "main", v1alpha1.ModeApply), credentials(), theirs)
	if _, err := reconcile(t, r, "main"); err != nil {
		t.Fatalf("a taken name is not retried with backoff: %v", err)
	}
	if d := condition(t, get(t, c, "main"), operator.Degraded); d.Status != metav1.ConditionTrue || d.Reason != "OutputsConflict" || !strings.Contains(d.Message, "Secret logto-secrets") {
		t.Fatalf("Degraded = %+v", d)
	}
	var s corev1.Secret
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "logto-secrets"}, &s); err != nil || string(s.Data["keep"]) != "me" || len(s.Data) != 1 {
		t.Fatalf("the other Secret was touched: %v %v", keys(s.Data), err)
	}
}

// The outputs follow the document: an output it no longer declares leaves the ConfigMap.
func TestAnOutputTheDocumentDropsIsRemoved(t *testing.T) {
	_, lc := kindtest.Fake(t)
	cfg := doc(t, lc.Endpoint(), "main", v1alpha1.ModeApply)
	r, c := setup(t, cfg, credentials())
	if _, err := reconcile(t, r, "main"); err != nil {
		t.Fatal(err)
	}
	latest := get(t, c, "main")
	latest.Spec.Resources = nil
	if err := c.Update(context.Background(), latest); err != nil {
		t.Fatal(err)
	}
	if _, err := reconcile(t, r, "main"); err != nil {
		t.Fatal(err)
	}
	var ids corev1.ConfigMap
	if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: "logto-ids"}, &ids); err != nil {
		t.Fatal(err)
	}
	if _, ok := ids.Data["resources.app.indicator"]; ok {
		t.Fatalf("ids = %v", ids.Data)
	}
}

// One Logto has many spellings: case, default port, trailing slash and scheme do not make another endpoint.
func TestAnEndpointIsComparedNormalized(t *testing.T) {
	key := func(endpoint string) string {
		return operator.EndpointOf(&v1alpha1.LogtoConfig{Spec: v1alpha1.LogtoConfigSpec{Endpoint: endpoint}})[0]
	}
	want := key("https://auth.example.test")
	for _, e := range []string{"https://Auth.Example.TEST/", "https://auth.example.test:443", "http://auth.example.test:80/", "HTTPS://auth.example.test//"} {
		if got := key(e); got != want {
			t.Errorf("%s = %q, want %q", e, got, want)
		}
	}
	if key("https://auth.example.test:3001") == want || key("https://auth.example.test/tenant") == want {
		t.Error("another port or path is another endpoint")
	}
}

// When a LogtoConfig goes away (or changes endpoint), the others of its endpoint are reconciled at once: one of
// them takes the endpoint over without waiting for its resync.
func TestTheOthersOfAnEndpointAreWokenUp(t *testing.T) {
	_, lc := kindtest.Fake(t)
	first := doc(t, lc.Endpoint(), "first", v1alpha1.ModeObserve)
	second := doc(t, lc.Endpoint()+"/", "second", v1alpha1.ModeObserve)
	other := doc(t, "https://elsewhere.example.test", "other", v1alpha1.ModeObserve)
	r, _ := setup(t, first, second, other, credentials())
	got := r.Siblings(context.Background(), first)
	if len(got) != 1 || got[0].Name != "second" {
		t.Fatalf("woken = %v", got)
	}
}
