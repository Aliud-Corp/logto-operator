// Package logto embeds the vendored Logto OpenAPI documents of the support window (versions.txt), as served and
// normalized, for the tests (contract tests, the fake). Nothing in the logtoctl binary imports it: the binary knows
// the window by fingerprints only (internal/logto/version).
package logto

import (
	"embed"
	"strings"
)

//go:embed versions.txt *.openapi.json *.normalized.json
var files embed.FS

// Versions is the support window, newest first.
func Versions() []string {
	raw, err := files.ReadFile("versions.txt")
	if err != nil {
		panic(err) // embedded at build time
	}
	var out []string
	for _, line := range strings.Split(string(raw), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

// Spec is the vendored document of one version.
func Spec(version string) ([]byte, error) {
	return files.ReadFile(version + ".openapi.json")
}

// Normalized is the vendored document of one version as openapi.Normalize turns it, written by `task generate`: what
// the tests load (openapi.LoadNormalized), so that they skip the costly normalization.
func Normalized(version string) ([]byte, error) {
	return files.ReadFile(version + ".normalized.json")
}
