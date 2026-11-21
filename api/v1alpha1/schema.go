package v1alpha1

import _ "embed"

// JSONSchema is the JSON Schema of a LogtoConfig document, generated from these types (task generate, the same rules
// as the CRD): the CLI validates a document against it before anything else.
//
//go:embed logtoconfig.schema.json
var JSONSchema []byte
