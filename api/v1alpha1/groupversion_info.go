// Package v1alpha1 is the schema of a LogtoConfig: the file format of logtoctl and the spec of the operator's CRD.
// The Go types are the source; controller-gen generates the deepcopy code and the CRD (config/crd/), and
// tools/crd2schema extracts the JSON Schema of the CLI file (logtoconfig.schema.json). All three are committed and
// guarded by `task generate:check`.
//
// +kubebuilder:object:generate=true
// +groupName=logto.aliud.fr
package v1alpha1

import "k8s.io/apimachinery/pkg/runtime/schema"

// GroupVersion is the API group and version of LogtoConfig.
var GroupVersion = schema.GroupVersion{Group: "logto.aliud.fr", Version: "v1alpha1"}

// Kind is the kind of a LogtoConfig document.
const Kind = "LogtoConfig"
