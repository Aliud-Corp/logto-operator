// Command logto-operator reconciles LogtoConfig objects with their Logto, on the engine of logtoctl
// (internal/operator).
package main

import (
	"flag"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	"github.com/Aliud-Corp/logto-operator/api/v1alpha1"
	"github.com/Aliud-Corp/logto-operator/internal/operator"
)

// version is set at build time (-ldflags "-X main.version=...").
var version = "dev"

func main() {
	var metrics, probes string
	var leaderElect bool
	flag.StringVar(&metrics, "metrics-bind-address", ":8080", "where the metrics are served; 0 turns them off")
	flag.StringVar(&probes, "health-probe-bind-address", ":8081", "where /healthz and /readyz are served")
	flag.BoolVar(&leaderElect, "leader-elect", false, "elect one active replica (run more than one for availability)")
	opts := zap.Options{}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	log := ctrl.Log.WithName("logto-operator")

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		log.Error(err, "scheme")
		os.Exit(1)
	}
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		log.Error(err, "scheme")
		os.Exit(1)
	}
	managed := labels.SelectorFromSet(labels.Set{operator.ManagedBy: operator.ManagedByValue})
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metrics},
		HealthProbeBindAddress: probes,
		LeaderElection:         leaderElect,
		LeaderElectionID:       "logto-operator.logto.aliud.fr",
		// The cache holds only the outputs the operator writes, never every Secret of the cluster.
		Cache: cache.Options{ByObject: map[client.Object]cache.ByObject{
			&corev1.Secret{}:    {Label: managed},
			&corev1.ConfigMap{}: {Label: managed},
		}},
	})
	if err != nil {
		log.Error(err, "manager")
		os.Exit(1)
	}
	if err := (&operator.Reconciler{Client: mgr.GetClient(), Scheme: scheme}).SetupWithManager(mgr); err != nil {
		log.Error(err, "controller")
		os.Exit(1)
	}
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		log.Error(err, "healthz")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		log.Error(err, "readyz")
		os.Exit(1)
	}
	log.Info("starting", "version", version)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Error(err, "manager stopped")
		os.Exit(1)
	}
}
