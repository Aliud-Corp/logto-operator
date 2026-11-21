// Package logto embeds the vendored Logto OpenAPI documents of the support window (versions.txt): the contract tests
// validate against them, and logtoctl recognises the version of the Logto it talks to by its own spec (Match).
package logto

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
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

// Match returns the supported version whose vendored spec is the one given (a target's own spec), or "" when none
// is: an untested Logto. Logto exposes no version number through its API, and its spec says `info.version: Cloud`;
// the whole document, compared without `servers` (it echoes the request's host), is the version.
func Match(raw []byte) (string, error) {
	want, err := fingerprint(raw)
	if err != nil {
		return "", fmt.Errorf("the target's API spec: %w", err)
	}
	vendored.once.Do(func() {
		vendored.prints = map[string]string{}
		for _, v := range Versions() {
			b, err := Spec(v)
			if err != nil {
				vendored.err = err
				return
			}
			if vendored.prints[v], err = fingerprint(b); err != nil {
				vendored.err = fmt.Errorf("vendored spec %s: %w", v, err)
				return
			}
		}
	})
	if vendored.err != nil {
		return "", vendored.err
	}
	for _, v := range Versions() {
		if vendored.prints[v] == want {
			return v, nil
		}
	}
	return "", nil
}

var vendored struct {
	once   sync.Once
	prints map[string]string
	err    error
}

// fingerprint is the SHA-256 of a spec in one canonical form: keys sorted, numbers as written, no HTML escaping,
// without `servers`.
func fingerprint(raw []byte) (string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return "", err
	}
	delete(doc, "servers")
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(doc); err != nil {
		return "", err
	}
	sum := sha256.Sum256(b.Bytes())
	return fmt.Sprintf("%x", sum), nil
}
