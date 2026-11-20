package config

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/Aliud-Corp/logto-operator/api/v1alpha1"
)

var (
	typeOfConfig      = reflect.TypeFor[v1alpha1.LogtoConfig]()
	typeOfUnmarshaler = reflect.TypeFor[json.Unmarshaler]()
)

// unknownFields walks the decoded tree against the Go type and reports every key the type does not have, with its
// path (encoding/json's DisallowUnknownFields names the key but not where it is).
func unknownFields(node any, t reflect.Type, path string) []error {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	// A type with its own decoding (Free, metav1.Time, ...) is opaque here.
	if reflect.PointerTo(t).Implements(typeOfUnmarshaler) {
		return nil
	}
	switch t.Kind() {
	case reflect.Struct:
		obj, ok := node.(map[string]any)
		if !ok {
			return nil // a type mismatch is reported by the decoder
		}
		fields := jsonFields(t)
		var errs []error
		for _, k := range sortedKeys(obj) {
			ft, ok := fields[k]
			if !ok {
				errs = append(errs, fmt.Errorf("%s: unknown field", join(path, k)))
				continue
			}
			errs = append(errs, unknownFields(obj[k], ft, join(path, k))...)
		}
		return errs
	case reflect.Map:
		obj, ok := node.(map[string]any)
		if !ok {
			return nil
		}
		var errs []error
		for _, k := range sortedKeys(obj) {
			errs = append(errs, unknownFields(obj[k], t.Elem(), join(path, k))...)
		}
		return errs
	case reflect.Slice:
		list, ok := node.([]any)
		if !ok {
			return nil
		}
		var errs []error
		for i, item := range list {
			errs = append(errs, unknownFields(item, t.Elem(), fmt.Sprintf("%s[%d]", path, i))...)
		}
		return errs
	default:
		return nil
	}
}

// jsonFields maps the JSON names of a struct's fields to their types, flattening inline (embedded) structs.
func jsonFields(t reflect.Type) map[string]reflect.Type {
	out := map[string]reflect.Type{}
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if f.Anonymous && (name == "" || strings.Contains(opts, "inline")) {
			ft := f.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			for k, v := range jsonFields(ft) {
				out[k] = v
			}
			continue
		}
		if name == "" {
			name = f.Name
		}
		out[name] = f.Type
	}
	return out
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}
