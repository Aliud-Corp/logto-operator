//go:build envtest

package manifests_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/Aliud-Corp/logto-operator/internal/operator"
)

// The install manifests (kustomize build config/default) against a real API server: every object is accepted with
// strict field validation, and the manager, run as the manifests' ServiceAccount, syncs its caches and takes the
// lease. A missing or too narrow RBAC rule fails here, not in someone's cluster.
func TestTheInstallManifestsAgainstARealAPIServer(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		if os.Getenv("LOGTO_ENVTEST_REQUIRED") != "" {
			t.Fatal("KUBEBUILDER_ASSETS is unset: run task test:envtest")
		}
		t.Skip("KUBEBUILDER_ASSETS is unset: run task test:envtest")
	}
	rendered, err := exec.CommandContext(t.Context(), "kubectl", "kustomize", filepath.Join("..", "..", "config", "default")).Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			t.Fatalf("kubectl kustomize: %v\n%s", err, exit.Stderr)
		}
		t.Fatalf("kubectl kustomize: %v", err)
	}
	env := &envtest.Environment{}
	cfg, err := env.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = env.Stop() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	scheme := runtime.NewScheme()
	if err := appsv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := rbacv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	admin, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}

	var deployments []*unstructured.Unstructured
	var crds []*apiextensionsv1.CustomResourceDefinition
	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(rendered), 4096)
	for {
		obj := &unstructured.Unstructured{}
		if err := decoder.Decode(&obj.Object); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if len(obj.Object) == 0 {
			continue
		}
		if err := admin.Create(ctx, obj, client.FieldValidation("Strict")); err != nil {
			t.Fatalf("%s %s: %v", obj.GetKind(), obj.GetName(), err)
		}
		if obj.GetKind() == "CustomResourceDefinition" {
			var crd apiextensionsv1.CustomResourceDefinition
			if err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, &crd); err != nil {
				t.Fatal(err)
			}
			crds = append(crds, &crd)
		}
		if obj.GetKind() == "Deployment" {
			deployments = append(deployments, obj)
		}
	}
	// The kind must be served before the manager watches it.
	if err := envtest.WaitForCRDs(cfg, crds, envtest.CRDInstallOptions{MaxTime: 30 * time.Second, PollInterval: 200 * time.Millisecond}); err != nil {
		t.Fatal(err)
	}
	if len(deployments) != 1 {
		t.Fatalf("%d Deployments, want 1", len(deployments))
	}
	var d appsv1.Deployment
	if err := admin.Get(ctx, client.ObjectKeyFromObject(deployments[0]), &d); err != nil {
		t.Fatal(err)
	}
	pod := d.Spec.Template.Spec
	if len(pod.Containers) != 1 || !strings.HasPrefix(pod.Containers[0].Image, "ko://github.com/Aliud-Corp/logto-operator/cmd/logto-operator") {
		t.Fatalf("containers = %+v", pod.Containers)
	}
	c := pod.Containers[0]
	if sc := c.SecurityContext; sc == nil || sc.ReadOnlyRootFilesystem == nil || !*sc.ReadOnlyRootFilesystem ||
		sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation || pod.SecurityContext == nil ||
		pod.SecurityContext.RunAsNonRoot == nil || !*pod.SecurityContext.RunAsNonRoot {
		t.Fatalf("security contexts = %+v, %+v", pod.SecurityContext, c.SecurityContext)
	}

	// The manager as the Pod would run it: its ServiceAccount's rights only, the lease in its namespace.
	sa := rest.CopyConfig(cfg)
	sa.Impersonate = rest.ImpersonationConfig{UserName: "system:serviceaccount:" + d.Namespace + ":" + pod.ServiceAccountName}
	mgr, err := operator.NewManager(sa, operator.Options{MetricsAddr: "0", ProbeAddr: "0", LeaderElect: true, LeaderElectionNamespace: d.Namespace})
	if err != nil {
		t.Fatal(err)
	}
	errc := make(chan error, 1)
	go func() { errc <- mgr.Start(ctx) }()
	synced, cancelSync := context.WithTimeout(ctx, 30*time.Second)
	defer cancelSync()
	select {
	case <-mgr.Elected():
	case err := <-errc:
		t.Fatalf("the manager stopped: %v", err)
	case <-synced.Done():
		t.Fatal("the manager never took the lease")
	}
	if !mgr.GetCache().WaitForCacheSync(synced) {
		t.Fatal("the caches never synced as the ServiceAccount")
	}
}
