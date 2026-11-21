// Package logto embeds the vendored Logto OpenAPI documents of the support window (versions.txt), for the contract
// tests. Nothing in the logtoctl binary imports it.
package logto

import (
	"embed"
	"strings"
)

//go:embed versions.txt *.openapi.json
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
