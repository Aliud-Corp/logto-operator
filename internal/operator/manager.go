package operator

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/Aliud-Corp/logto-operator/api/v1alpha1"
)

// Options of the manager.
type Options struct {
	MetricsAddr string // "0" turns the metrics off
	ProbeAddr   string
	LeaderElect bool
	// LeaderElectionNamespace holds the lease; the Pod's namespace when empty (in a cluster).
	LeaderElectionNamespace string
	Concurrency             int
}

// NewManager is the operator's manager, its controller registered: what cmd/logto-operator runs, and what the
// envtest suite starts against a real API server.
func NewManager(cfg *rest.Config, o Options) (ctrl.Manager, error) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return nil, err
	}
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		return nil, err
	}
	managed := labels.SelectorFromSet(labels.Set{ManagedBy: ManagedByValue})
	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:                  scheme,
		Metrics:                 metricsserver.Options{BindAddress: o.MetricsAddr},
		HealthProbeBindAddress:  o.ProbeAddr,
		LeaderElection:          o.LeaderElect,
		LeaderElectionID:        "logto-operator.logto.aliud.fr",
		LeaderElectionNamespace: o.LeaderElectionNamespace,
		// The cache holds only the outputs the operator writes, never every Secret of the cluster.
		Cache: cache.Options{ByObject: map[client.Object]cache.ByObject{
			&corev1.Secret{}:    {Label: managed},
			&corev1.ConfigMap{}: {Label: managed},
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("manager: %w", err)
	}
	if err := (&Reconciler{Client: mgr.GetClient(), Scheme: scheme, Concurrency: o.Concurrency}).SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("controller: %w", err)
	}
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return nil, err
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return nil, err
	}
	return mgr, nil
}
