// Command specnorm writes the normalized form of a vendored Logto spec, the input of oapi-codegen
// (internal/logto/specnorm says what it rewrites and why).
//
// Usage: go run ./tools/specnorm <in.json> <out.json>
package main

import (
	"fmt"
	"os"

	"github.com/Aliud-Corp/logto-operator/internal/logto/specnorm"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: specnorm <in.json> <out.json>")
		os.Exit(2)
	}
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "specnorm:", err)
		os.Exit(1)
	}
	out, rewritten, err := specnorm.Normalize(raw, specnorm.Overlays)
	if err != nil {
		fmt.Fprintln(os.Stderr, "specnorm:", err)
		os.Exit(1)
	}
	if rewritten == 0 {
		fmt.Fprintln(os.Stderr, "specnorm: no literal `format` found; this normalization step may no longer be needed")
	}
	if err := os.WriteFile(os.Args[2], out, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "specnorm:", err)
		os.Exit(1)
	}
}
