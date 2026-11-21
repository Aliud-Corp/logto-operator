// Package version recognises the Logto version a target runs, by the API spec it serves. Logto exposes no version
// number through its API, and its spec says `info.version: Cloud`; the whole document, compared without `servers`
// (it echoes the request's host), is the version. Only the fingerprints of the vendored specs are compiled in
// (fingerprints.gen.go, written by `task generate`): the specs themselves stay out of the binary.
package version

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// Versions is the support window, newest first.
func Versions() []string {
	out := make([]string, len(supported))
	for i, s := range supported {
		out[i] = s.version
	}
	return out
}

// Match returns the supported version whose vendored spec is the one given (a target's own), or "" when none is: an
// untested Logto.
func Match(raw []byte) (string, error) {
	got, err := Fingerprint(raw)
	if err != nil {
		return "", fmt.Errorf("the target's API spec: %w", err)
	}
	for _, s := range supported {
		if s.fingerprint == got {
			return s.version, nil
		}
	}
	return "", nil
}

// Fingerprint is the SHA-256 of a spec in one canonical form: keys sorted, numbers as written, no HTML escaping,
// without `servers`.
func Fingerprint(raw []byte) (string, error) {
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
	return fmt.Sprintf("%x", sha256.Sum256(b.Bytes())), nil
}
