//go:build envtest

package operator_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/Aliud-Corp/logto-operator/api/v1alpha1"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindtest"
	"github.com/Aliud-Corp/logto-operator/internal/operator"
)

// The manager of cmd/logto-operator (NewManager: scheme, label-filtered cache, index, predicates, owned objects)
// against a real API server (envtest) and the fake Logto: what the fake client cannot show.
func TestTheManagerAgainstARealAPIServer(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		if os.Getenv("LOGTO_ENVTEST_REQUIRED") != "" {
			t.Fatal("KUBEBUILDER_ASSETS is unset: run task test:envtest")
		}
		t.Skip("KUBEBUILDER_ASSETS is unset: run task test:envtest")
	}
	env := &envtest.Environment{CRDDirectoryPaths: []string{filepath.Join("..", "..", "config", "crd")}, ErrorIfCRDPathMissing: true}
	cfg, err := env.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = env.Stop() })

	mgr, err := operator.NewManager(cfg, operator.Options{MetricsAddr: "0", ProbeAddr: "0"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = mgr.Start(ctx) }()

	c, err := client.New(cfg, client.Options{Scheme: mgr.GetScheme()})
	if err != nil {
		t.Fatal(err)
	}
	fakeLogto, lc := kindtest.Fake(t)
	if err := c.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}}); err != nil {
		t.Fatal(err)
	}
	if err := c.Create(ctx, credentials(lc.Endpoint())); err != nil {
		t.Fatal(err)
	}
	// An object of the outputs' name that the operator does not manage: outside its cache, it must still be refused.
	if err := c.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "taken", Namespace: ns}}); err != nil {
		t.Fatal(err)
	}

	main := doc(t, lc.Endpoint(), "main", v1alpha1.ModeApply)
	main.ResourceVersion, main.Generation = "", 0
	if err := c.Create(ctx, main); err != nil {
		t.Fatal(err)
	}
	ready := waitFor(t, c, "main", func(cfg *v1alpha1.LogtoConfig) bool {
		return apimeta.IsStatusConditionTrue(cfg.Status.Conditions, operator.Ready)
	})
	if ready.Status.LogtoVersion == "" || ready.Status.LastAppliedTime == nil {
		t.Fatalf("status = %+v", ready.Status)
	}
	var ids corev1.ConfigMap
	if err := c.Get(ctx, types.NamespacedName{Namespace: ns, Name: "logto-ids"}, &ids); err != nil || ids.Data["resources.app.indicator"] == "" {
		t.Fatalf("ids = %v, %v", ids.Data, err)
	}

	// Every run asks the fake for a token. Once settled, a status write (the operator's own kind of write) runs
	// nothing; an output that loses its label is taken back by the next run, never called someone else's.
	settled := func() int {
		t.Helper()
		for n := -1; ; {
			time.Sleep(time.Second)
			if now := fakeLogto.TokensIssued(); now == n {
				return n
			} else {
				n = now
			}
		}
	}
	runs := settled()
	latest := waitFor(t, c, "main", func(*v1alpha1.LogtoConfig) bool { return true })
	apimeta.SetStatusCondition(&latest.Status.Conditions, metav1.Condition{Type: "Probe", Status: metav1.ConditionTrue, Reason: "Test"})
	if err := c.Status().Update(ctx, latest); err != nil {
		t.Fatal(err)
	}
	if settled() != runs {
		t.Fatalf("a status write ran the engine %d times", fakeLogto.TokensIssued()-runs)
	}
	ids.Labels = nil
	if err := c.Update(ctx, &ids); err != nil {
		t.Fatal(err)
	}
	latest = waitFor(t, c, "main", func(*v1alpha1.LogtoConfig) bool { return true })
	latest.Annotations = map[string]string{"touch": "1"}
	if err := c.Update(ctx, latest); err != nil {
		t.Fatal(err)
	}
	waitFor(t, c, "main", func(cfg *v1alpha1.LogtoConfig) bool {
		var cm corev1.ConfigMap
		_ = c.Get(ctx, types.NamespacedName{Namespace: ns, Name: "logto-ids"}, &cm)
		return cm.Labels[operator.ManagedBy] == operator.ManagedByValue && apimeta.IsStatusConditionTrue(cfg.Status.Conditions, operator.Ready)
	})

	// A second object on the endpoint is refused; when the first goes away, the second takes over at once.
	second := doc(t, lc.Endpoint()+"/", "second", v1alpha1.ModeObserve)
	second.ResourceVersion, second.Generation = "", 0
	second.Spec.Outputs = nil
	if err := c.Create(ctx, second); err != nil {
		t.Fatal(err)
	}
	waitFor(t, c, "second", func(cfg *v1alpha1.LogtoConfig) bool {
		d := apimeta.FindStatusCondition(cfg.Status.Conditions, operator.Degraded)
		return d != nil && d.Status == metav1.ConditionTrue && d.Reason == "EndpointTaken"
	})
	if err := c.Delete(ctx, ready); err != nil {
		t.Fatal(err)
	}
	waitFor(t, c, "second", func(cfg *v1alpha1.LogtoConfig) bool {
		return apimeta.IsStatusConditionFalse(cfg.Status.Conditions, operator.Degraded)
	})

	// An outputs name held by an object the cache does not see is refused, not retried for ever.
	taken := doc(t, "https://other.example.test", "taken", v1alpha1.ModeApply)
	taken.ResourceVersion, taken.Generation = "", 0
	taken.Spec.Endpoint = lc.Endpoint()
	taken.Spec.Outputs.Secret.Name = "taken"
	if err := c.Delete(ctx, second); err != nil {
		t.Fatal(err)
	}
	if err := c.Create(ctx, taken); err != nil {
		t.Fatal(err)
	}
	waitFor(t, c, "taken", func(cfg *v1alpha1.LogtoConfig) bool {
		d := apimeta.FindStatusCondition(cfg.Status.Conditions, operator.Degraded)
		return d != nil && d.Status == metav1.ConditionTrue && d.Reason == "OutputsConflict"
	})
}

func waitFor(t *testing.T, c client.Client, name string, ok func(*v1alpha1.LogtoConfig) bool) *v1alpha1.LogtoConfig {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	var cfg v1alpha1.LogtoConfig
	for time.Now().Before(deadline) {
		if err := c.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: name}, &cfg); err == nil && ok(&cfg) {
			return &cfg
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("%s never reached the expected state: %+v", name, cfg.Status)
	return nil
}
