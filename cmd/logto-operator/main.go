// Command logto-operator reconciles LogtoConfig objects with their Logto, on the engine of logtoctl
// (internal/operator, docs/operator.md).
package main

import (
	"flag"
	"os"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/Aliud-Corp/logto-operator/internal/operator"
)

// version is set at build time (-ldflags "-X main.version=...").
var version = "dev"

func main() {
	var o operator.Options
	flag.StringVar(&o.MetricsAddr, "metrics-bind-address", "0", "where the metrics are served, plain HTTP (keep it inside the cluster); 0 turns them off")
	flag.StringVar(&o.ProbeAddr, "health-probe-bind-address", ":8081", "where /healthz and /readyz are served")
	flag.BoolVar(&o.LeaderElect, "leader-elect", true, "elect one active replica: two would both write to Logto")
	flag.StringVar(&o.LeaderElectionNamespace, "leader-election-namespace", "", "where the lease lives; the Pod's namespace in a cluster (required outside one, or --leader-elect=false)")
	flag.IntVar(&o.Concurrency, "max-concurrent-reconciles", 4, "LogtoConfigs reconciled at once (runs on one endpoint take turns)")
	opts := zap.Options{}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	log := ctrl.Log.WithName("logto-operator")

	mgr, err := operator.NewManager(ctrl.GetConfigOrDie(), o)
	if err != nil {
		log.Error(err, "start")
		os.Exit(1)
	}
	log.Info("starting", "version", version)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Error(err, "manager stopped")
		os.Exit(1)
	}
}
