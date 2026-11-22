// Package config loads a LogtoConfig document the way a reviewer reads it: strictly (an unknown key is an error, with
// its path), with ${name} variables replaced from spec.variables only, and with every fromFile / fromDir resolved
// relative to the document. Validate then applies the schema rules no Logto call should be needed to catch.
package config

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"
	"sigs.k8s.io/yaml"

	"github.com/Aliud-Corp/logto-operator/api/v1alpha1"
	"github.com/Aliud-Corp/logto-operator/internal/logto/openapi"
)

// Document is a loaded LogtoConfig and the directory its relative paths resolve against.
type Document struct {
	Config *v1alpha1.LogtoConfig
	Dir    string
}

// Load reads, interpolates, strictly decodes and resolves the document at path. It does not validate: call Validate.
func Load(path string) (*Document, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dir, err := filepath.Abs(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	cfg, err := Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := resolveFiles(cfg, dir); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &Document{Config: cfg, Dir: dir}, nil
}

// Parse turns YAML (or JSON) into a LogtoConfig: variables interpolated, unknown fields refused, then every rule of
// the generated JSON Schema checked on the document as written (what the CRD refuses: required fields, patterns,
// enums, minimums), all violations at once. Files are not read. The rules across fields are Validate's.
func Parse(raw []byte) (*v1alpha1.LogtoConfig, error) {
	js, err := yaml.YAMLToJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("not YAML: %w", err)
	}
	var tree any
	if err := json.Unmarshal(js, &tree); err != nil {
		return nil, fmt.Errorf("not a document: %w", err)
	}
	root, ok := tree.(map[string]any)
	if !ok {
		return nil, errors.New("the document is not an object")
	}
	if err := interpolate(root); err != nil {
		return nil, err
	}
	if errs := unknownFields(root, typeOfConfig, ""); len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	if root["apiVersion"] != v1alpha1.GroupVersion.String() || root["kind"] != v1alpha1.Kind {
		return nil, fmt.Errorf("apiVersion/kind is %q/%q, want %q/%q", root["apiVersion"], root["kind"], v1alpha1.GroupVersion.String(), v1alpha1.Kind)
	}
	if errs := schemaRules(root); len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	js, err = json.Marshal(root)
	if err != nil {
		return nil, err
	}
	var cfg v1alpha1.LogtoConfig
	if err := json.Unmarshal(js, &cfg); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	if want := v1alpha1.GroupVersion.String(); cfg.APIVersion != want || cfg.Kind != v1alpha1.Kind {
		return nil, fmt.Errorf("apiVersion/kind is %q/%q, want %q/%q", cfg.APIVersion, cfg.Kind, want, v1alpha1.Kind)
	}
	return &cfg, nil
}

var varPattern = regexp.MustCompile(`\$\$\{|\$\{([^}]*)\}`)

// interpolate replaces ${name} in every string under spec (except spec.variables itself) with spec.variables[name].
// $${ writes a literal ${. An unknown or malformed name is an error naming the field.
func interpolate(root map[string]any) error {
	spec, _ := root["spec"].(map[string]any)
	if spec == nil {
		return nil
	}
	vars := map[string]string{}
	if raw, ok := spec["variables"].(map[string]any); ok {
		for k, v := range raw {
			s, ok := v.(string)
			if !ok {
				return fmt.Errorf("spec.variables.%s: must be a string", k)
			}
			vars[k] = s
		}
	}
	var errs []error
	var walk func(node any, path string) any
	walk = func(node any, path string) any {
		switch v := node.(type) {
		case map[string]any:
			for _, k := range sortedKeys(v) {
				if path == "spec" && k == "variables" {
					continue
				}
				v[k] = walk(v[k], path+"."+k)
			}
			return v
		case []any:
			for i := range v {
				v[i] = walk(v[i], fmt.Sprintf("%s[%d]", path, i))
			}
			return v
		case string:
			return varPattern.ReplaceAllStringFunc(v, func(m string) string {
				if m == "$${" {
					return "${"
				}
				name := m[2 : len(m)-1]
				val, ok := vars[name]
				if !ok {
					errs = append(errs, fmt.Errorf("%s: ${%s} is not declared in spec.variables", path, name))
					return m
				}
				return val
			})
		default:
			return v
		}
	}
	walk(spec, "spec")
	return errors.Join(errs...)
}

// resolveFiles reads every fromFile / fromDir relative to dir.
func resolveFiles(cfg *v1alpha1.LogtoConfig, dir string) error {
	var errs []error
	text := func(path string, ts *v1alpha1.TextSource) {
		if ts == nil {
			return
		}
		if err := resolveText(ts, dir); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, err))
		}
	}
	s := &cfg.Spec
	if sie := s.SignInExperience; sie != nil {
		text("spec.signInExperience.customCss", sie.CustomCSS)
		if b := sie.Branding; b != nil {
			text("spec.signInExperience.branding.logoUrl", b.LogoURL)
			text("spec.signInExperience.branding.darkLogoUrl", b.DarkLogoURL)
			text("spec.signInExperience.branding.favicon", b.Favicon)
		}
	}
	for _, key := range sortedKeys(s.Connectors) {
		c := s.Connectors[key]
		if c.SMTP == nil {
			continue
		}
		path := "spec.connectors." + key + ".smtp.templates"
		if err := resolveTemplates(&c.SMTP.Templates, dir); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", path, err))
		}
		for i := range c.SMTP.Templates.Items {
			text(fmt.Sprintf("%s.items[%d].content", path, i), &c.SMTP.Templates.Items[i].Content)
		}
		s.Connectors[key] = c
	}
	return errors.Join(errs...)
}

func resolveText(ts *v1alpha1.TextSource, dir string) error {
	if ts.FromFile == "" {
		if ts.As == "dataUri" {
			return errors.New("as: dataUri needs fromFile")
		}
		return nil
	}
	if ts.Value != "" {
		return errors.New("value and fromFile are exclusive")
	}
	b, err := os.ReadFile(filepath.Join(dir, ts.FromFile))
	if err != nil {
		return fmt.Errorf("fromFile: %w", err)
	}
	if ts.As == "dataUri" {
		mt := mime.TypeByExtension(filepath.Ext(ts.FromFile))
		if mt == "" {
			return fmt.Errorf("fromFile %s: unknown media type for a data URI", ts.FromFile)
		}
		mt, _, _ = strings.Cut(mt, ";")
		ts.Value = "data:" + mt + ";base64," + base64.StdEncoding.EncodeToString(b)
	} else {
		ts.Value = string(b)
	}
	ts.FromFile, ts.As = "", ""
	return nil
}

// resolveTemplates reads <dir>/<fromDir>/*.json, one template per file, sorted by file name, appended to Items.
func resolveTemplates(t *v1alpha1.EmailTemplates, dir string) error {
	if t.FromDir == "" {
		return nil
	}
	files, err := filepath.Glob(filepath.Join(dir, t.FromDir, "*.json"))
	if err != nil {
		return err
	}
	if len(files) == 0 {
		return fmt.Errorf("fromDir %s: no *.json template", t.FromDir)
	}
	sort.Strings(files)
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		var item v1alpha1.EmailTemplate
		dec := json.NewDecoder(strings.NewReader(string(b)))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&item); err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(f), err)
		}
		t.Items = append(t.Items, item)
	}
	t.FromDir = ""
	return nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

var docSchema = sync.OnceValues(func() (*openapi3.Schema, error) {
	var raw map[string]any
	if err := json.Unmarshal(v1alpha1.JSONSchema, &raw); err != nil {
		return nil, err
	}
	delete(raw, "$schema")
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var s openapi3.Schema
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
})

// schemaRules checks the document against the generated JSON Schema; each violation carries its path.
func schemaRules(root map[string]any) []error {
	s, err := docSchema()
	if err != nil {
		return []error{fmt.Errorf("the document schema: %w", err)}
	}
	var errs []error
	for _, p := range openapi.Validate(s, root) {
		errs = append(errs, errors.New(p))
	}
	return errs
}
