package operator_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/go-logr/logr/funcr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/Aliud-Corp/logto-operator/api/v1alpha1"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindtest"
	"github.com/Aliud-Corp/logto-operator/internal/operator"
	"github.com/Aliud-Corp/logto-operator/internal/secrets"
)

// sink captures what a reconcile logs, one JSON object per line.
type sink struct {
	mu    sync.Mutex
	raw   []string
	lines []map[string]any
}

func (s *sink) reconcile(t *testing.T, r *operator.Reconciler, name string) (ctrl.Result, error) {
	t.Helper()
	logger := funcr.NewJSON(func(obj string) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.raw = append(s.raw, obj)
		var line map[string]any
		if err := json.Unmarshal([]byte(obj), &line); err != nil {
			t.Errorf("a log line is not JSON: %v: %s", err, obj)
		}
		s.lines = append(s.lines, line)
	}, funcr.Options{})
	ctx := log.IntoContext(context.Background(), logger)
	return r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: name}})
}

// only is the one line a reconcile logged.
func (s *sink) only(t *testing.T) map[string]any {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.lines) != 1 {
		t.Fatalf("a reconcile logs one line, got %d: %v", len(s.raw), s.raw)
	}
	line := s.lines[0]
	s.raw, s.lines = nil, nil
	return line
}

func want(t *testing.T, line map[string]any, fields map[string]any) {
	t.Helper()
	for k, v := range fields {
		if line[k] != v {
			t.Errorf("%s = %#v, want %#v (line %v)", k, line[k], v, line)
		}
	}
}

// Every reconcile logs one line: which object, which endpoint, which mode, what came of it and when it comes back. A
// resync is visible without reading the status.
func TestAReconcileLogsOneLine(t *testing.T) {
	_, lc := kindtest.Fake(t)
	r, _ := setup(t, doc(t, lc.Endpoint(), "main", v1alpha1.ModeApply), credentials(lc.Endpoint()))
	var s sink
	if _, err := s.reconcile(t, r, "main"); err != nil {
		t.Fatal(err)
	}
	line := s.only(t)
	want(t, line, map[string]any{
		"msg": "reconciled", "logtoConfig": "tenant/main", "mode": v1alpha1.ModeApply, "generation": float64(2),
		"outcome": "Applied", "created": float64(2), "updated": float64(0), "deleted": float64(0),
		"requeueAfter": "10m0s",
	})
	if line["endpoint"] == "" || line["endpoint"] == nil || line["logtoVersion"] == "" || line["logtoVersion"] == nil {
		t.Errorf("no endpoint or Logto version: %v", line)
	}

	if _, err := s.reconcile(t, r, "main"); err != nil {
		t.Fatal(err)
	}
	want(t, s.only(t), map[string]any{"outcome": "InSync", "created": float64(0), "requeueAfter": "10m0s"})
}

// Observe logs what differs, in counts.
func TestAnObserveRunLogsItsDrift(t *testing.T) {
	_, lc := kindtest.Fake(t)
	r, _ := setup(t, doc(t, lc.Endpoint(), "main", v1alpha1.ModeObserve), credentials(lc.Endpoint()))
	var s sink
	if _, err := s.reconcile(t, r, "main"); err != nil {
		t.Fatal(err)
	}
	want(t, s.only(t), map[string]any{"mode": v1alpha1.ModeObserve, "outcome": "Drifted", "created": float64(2)})
}

// A degraded run logs its reason and its error: a Secret not there yet shows up as such, and is looked for again
// soon, not at the next resync.
func TestADegradedRunLogsItsReason(t *testing.T) {
	_, lc := kindtest.Fake(t)
	r, _ := setup(t, doc(t, lc.Endpoint(), "main", v1alpha1.ModeApply))
	var s sink
	if _, err := s.reconcile(t, r, "main"); err != nil {
		t.Fatal(err)
	}
	line := s.only(t)
	want(t, line, map[string]any{"outcome": "Degraded", "reason": "SecretNotReadable", "requeueAfter": operator.SecretRetry.String()})
	if msg, _ := line["error"].(string); !strings.Contains(msg, `secrets "logto-credentials" not found`) {
		t.Errorf("error = %q", line["error"])
	}
}

// A transient run logs that it is retried with backoff, not the resync.
func TestATransientRunLogsTheBackoff(t *testing.T) {
	fakeLogto, lc := kindtest.Fake(t)
	fakeLogto.FailNext("POST /api/resources", 503)
	r, _ := setup(t, doc(t, lc.Endpoint(), "main", v1alpha1.ModeApply), credentials(lc.Endpoint()))
	var s sink
	if _, err := s.reconcile(t, r, "main"); err == nil {
		t.Fatal("a transient failure is returned")
	}
	want(t, s.only(t), map[string]any{"outcome": "Transient", "requeueAfter": "backoff"})
}

// An error that carries a Secret value (an API server echoing what it refused) is logged redacted, like the status.
func TestTheLogHoldsNoSecretValue(t *testing.T) {
	_, lc := kindtest.Fake(t)
	const value = "boot-secret" // the credentials' client secret
	funcs := interceptor.Funcs{Create: func(ctx context.Context, c client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
		if _, ok := obj.(*corev1.ConfigMap); ok {
			return errors.New("admission refused: data holds " + value)
		}
		return c.Create(ctx, obj, opts...)
	}}
	r, _ := setupWith(t, funcs, doc(t, lc.Endpoint(), "main", v1alpha1.ModeApply), credentials(lc.Endpoint()))
	var s sink
	_, _ = s.reconcile(t, r, "main")
	raw := strings.Join(s.raw, "\n")
	line := s.only(t)
	if strings.Contains(raw, value) {
		t.Fatalf("the log holds a Secret value: %s", raw)
	}
	if msg, _ := line["error"].(string); !strings.Contains(msg, secrets.Masked) {
		t.Fatalf("error = %q, want it masked", line["error"])
	}
}
