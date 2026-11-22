// Package outputs writes what an apply publishes: identifiers (application ids, resource indicators), printable, and
// secrets (M2M secrets), never printed. Identifiers go to stdout as KEY=value lines or JSON; secrets go to a file
// created with mode 0600 (enforced on an existing file) or to an inherited file descriptor, as JSON.
package outputs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/Aliud-Corp/logto-operator/internal/engine"
)

// WriteIDs writes the identifiers, sorted, as "name=value" lines (format "env") or a JSON object (format "json").
func WriteIDs(w io.Writer, o *engine.Outputs, format string) error {
	ids := map[string]string{}
	if o != nil {
		ids = o.IDs
	}
	switch format {
	case "json":
		if ids == nil {
			ids = map[string]string{}
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(ids)
	case "env", "":
		keys := make([]string, 0, len(ids))
		for k := range ids {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if _, err := fmt.Fprintf(w, "%s=%s\n", k, ids[k]); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown outputs format %q (env or json)", format)
	}
}

// Secrets returns the secrets as the JSON object written to a file or a descriptor.
func secretsJSON(o *engine.Outputs) ([]byte, error) {
	m := map[string]string{}
	if o != nil {
		for k, v := range o.Secrets {
			m[k] = v.Reveal()
		}
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// WriteSecretsFile writes the secrets to path, creating it 0600 and forcing 0600 on an existing file.
func WriteSecretsFile(o *engine.Outputs, path string) error {
	b, err := secretsJSON(o)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(b); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// WriteSecretsFD writes the secrets to an inherited descriptor (3 or above).
func WriteSecretsFD(o *engine.Outputs, fd int) error {
	if fd < 3 {
		return errors.New("--secrets-fd must be an inherited descriptor, 3 or above (0, 1 and 2 are the standard streams)")
	}
	f := os.NewFile(uintptr(fd), "secrets")
	if f == nil {
		return fmt.Errorf("descriptor %d is not open", fd)
	}
	if _, err := f.Stat(); err != nil {
		return fmt.Errorf("descriptor %d is not open: %w", fd, err)
	}
	b, err := secretsJSON(o)
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	return err
}
