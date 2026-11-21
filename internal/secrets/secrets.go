// Package secrets resolves the values a LogtoConfig reads from elsewhere (valueFrom) and keeps secrets from ever being
// printed: a Secret formats as "(sensitive)" through every verb and every encoder, and a Redactor removes known secret
// values from text that came from somewhere else (an error message of Logto, a log line).
package secrets

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/Aliud-Corp/logto-operator/api/v1alpha1"
)

// Masked is what a secret prints as.
const Masked = "(sensitive)"

// Secret holds a secret value. Only Reveal returns it.
type Secret struct{ v string }

// New wraps a value.
func New(v string) Secret { return Secret{v: v} }

// Reveal returns the value, for the one place that must send it.
func (s Secret) Reveal() string { return s.v }

// IsZero reports an empty secret.
func (s Secret) IsZero() bool { return s.v == "" }

func (Secret) String() string   { return Masked }
func (Secret) GoString() string { return Masked }

// Format masks the secret for every verb, %#v and %+v included.
func (Secret) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(Masked)) }

// MarshalJSON masks the secret in any JSON output.
func (Secret) MarshalJSON() ([]byte, error) { return json.Marshal(Masked) }

// MarshalText masks the secret in any text encoding (YAML, logs with text marshalling).
func (Secret) MarshalText() ([]byte, error) { return []byte(Masked), nil }

// Resolver reads valueFrom sources.
type Resolver struct {
	Getenv            func(string) (string, bool)
	ReadFile          func(string) ([]byte, error)
	AllowSecretKeyRef bool // the operator reads Secrets; the CLI never does
	Redactor          *Redactor
}

// CLI is the resolver of logtoctl: environment and files, never a Kubernetes Secret.
func CLI(r *Redactor) Resolver {
	return Resolver{Getenv: os.LookupEnv, ReadFile: os.ReadFile, Redactor: r}
}

// String resolves a value that may be written in the document or read from elsewhere.
func (r Resolver) String(path string, s v1alpha1.StringSource) (string, error) {
	if s.ValueFrom == nil {
		if s.Value == "" {
			return "", fmt.Errorf("%s: no value", path)
		}
		return s.Value, nil
	}
	return r.read(path+".valueFrom", *s.ValueFrom)
}

// Secret resolves a secret and registers it with the redactor.
func (r Resolver) Secret(path string, s v1alpha1.SecretSource) (Secret, error) {
	v, err := r.read(path+".valueFrom", s.ValueFrom)
	if err != nil {
		return Secret{}, err
	}
	sec := New(v)
	if r.Redactor != nil {
		r.Redactor.Add(sec)
	}
	return sec, nil
}

func (r Resolver) read(path string, f v1alpha1.ValueFrom) (string, error) {
	switch {
	case f.Env != "":
		v, ok := r.Getenv(f.Env)
		if !ok || v == "" {
			return "", fmt.Errorf("%s: environment variable %s is unset or empty", path, f.Env)
		}
		return v, nil
	case f.File != "":
		b, err := r.ReadFile(f.File)
		if err != nil {
			return "", fmt.Errorf("%s: %w", path, err)
		}
		v := strings.TrimSuffix(strings.TrimSuffix(string(b), "\n"), "\r")
		if v == "" {
			return "", fmt.Errorf("%s: file %s is empty", path, f.File)
		}
		return v, nil
	case f.SecretKeyRef != nil:
		if !r.AllowSecretKeyRef {
			return "", fmt.Errorf("%s: secretKeyRef is read by the operator only; logtoctl reads env or file", path)
		}
		return "", errors.New(path + ": secretKeyRef is not implemented yet (milestone 3)")
	default:
		return "", fmt.Errorf("%s: names no source", path)
	}
}

// Redactor knows the secret values of a run and removes them from text.
type Redactor struct {
	mu     sync.Mutex
	values []string
}

// Add registers a secret value (values shorter than 4 characters are not, to avoid masking common text).
func (r *Redactor) Add(s Secret) {
	if len(s.v) < 4 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.values = append(r.values, s.v)
}

// Redact replaces every registered value in text.
func (r *Redactor) Redact(text string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, v := range r.values {
		text = strings.ReplaceAll(text, v, Masked)
	}
	return text
}
