// Package cli is logtoctl: four verbs on one engine.
//
//	logtoctl render -f config.yaml   the document as it will be applied (files inlined, variables replaced)
//	logtoctl plan   -f config.yaml   what apply would do; writes nothing
//	logtoctl check  -f config.yaml   plan, exit 2 when Logto differs from the document (drift)
//	logtoctl apply  -f config.yaml   reconcile; identifiers on stdout, secrets to --secrets-out or --secrets-fd
//
// -f - reads the document on stdin, its files relative to the working directory.
//
// Exit codes: 0 done or in sync, 1 error, 2 drift (check), 3 applied with warnings (apply). Secrets never come from
// a flag: the document names them (valueFrom env or file).
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/Aliud-Corp/logto-operator/api/v1alpha1"
	"github.com/Aliud-Corp/logto-operator/internal/config"
	"github.com/Aliud-Corp/logto-operator/internal/engine"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds"
	"github.com/Aliud-Corp/logto-operator/internal/engine/plan"
	"github.com/Aliud-Corp/logto-operator/internal/outputs"
	"github.com/Aliud-Corp/logto-operator/internal/secrets"
	"github.com/Aliud-Corp/logto-operator/internal/target"
)

// Exit codes.
const (
	ExitOK       = 0
	ExitError    = 1
	ExitDrift    = 2
	ExitWarnings = 3
	exitUsage    = 2
)

// Usage is printed by -h and on a usage error.
const Usage = `usage: logtoctl <command> [flags]

commands:
  render   print the document as it will be applied (files inlined, variables replaced)
  plan     show what apply would do; writes nothing
  check    plan, and exit 2 when Logto differs from the document
  apply    reconcile Logto with the document
  version  print the version

flags (render, plan, check, apply):
  -f FILE            the LogtoConfig document (required); - reads it on stdin, its files relative to the
                     working directory
apply only:
  --outputs FORMAT   identifiers on stdout as env (name=value, default) or json
  --secrets-out FILE write published secrets (JSON) to FILE, created with mode 0600
  --secrets-fd N     write published secrets (JSON) to inherited descriptor N (3 or above)
  --print-secrets    print published secrets on stdout too (avoid)
plan, check, apply:
  --allow-untested-logto  run against a Logto whose API spec is none of the supported versions

exit codes: 0 done or in sync, 1 error, 2 drift (check) or usage error, 3 applied with warnings
`

// Run executes one command line and returns the exit code.
func Run(ctx context.Context, args []string, version string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = io.WriteString(stderr, Usage)
		return exitUsage
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "version":
		_, _ = fmt.Fprintln(stdout, version)
		return ExitOK
	case "-h", "--help", "help":
		_, _ = io.WriteString(stdout, Usage)
		return ExitOK
	case "render", "plan", "check", "apply":
	default:
		_, _ = fmt.Fprintf(stderr, "logtoctl: unknown command %q\n\n%s", cmd, Usage)
		return exitUsage
	}
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	file := fs.String("f", "", "the LogtoConfig document")
	format := fs.String("outputs", "env", "identifiers format: env or json")
	secretsOut := fs.String("secrets-out", "", "file for published secrets (0600)")
	secretsFD := fs.Int("secrets-fd", -1, "inherited descriptor for published secrets")
	printSecrets := fs.Bool("print-secrets", false, "print published secrets on stdout too")
	untested := fs.Bool("allow-untested-logto", false, "run against a Logto outside the support window")
	if err := fs.Parse(rest); err != nil {
		return exitUsage
	}
	if *file == "" || fs.NArg() > 0 {
		_, _ = fmt.Fprintf(stderr, "logtoctl %s: -f FILE is required, and nothing else\n", cmd)
		return exitUsage
	}
	if *secretsOut != "" && *secretsFD >= 0 {
		_, _ = fmt.Fprintln(stderr, "logtoctl: --secrets-out and --secrets-fd are exclusive")
		return exitUsage
	}
	// Checked before any call: a wrong flag found after the writes would publish nothing.
	if *format != "env" && *format != "json" {
		_, _ = fmt.Fprintf(stderr, "logtoctl: --outputs is env or json, not %q\n", *format)
		return exitUsage
	}
	if *secretsFD >= 0 && *secretsFD < 3 {
		_, _ = fmt.Fprintln(stderr, "logtoctl: --secrets-fd is an inherited descriptor, 3 or above (0, 1 and 2 are the standard streams)")
		return exitUsage
	}

	redactor := &secrets.Redactor{}
	fail := func(err error) int {
		msg := redactor.Redact(err.Error())
		if errors.Is(err, engine.ErrUntested) {
			msg += " (--allow-untested-logto)"
		}
		_, _ = fmt.Fprintf(stderr, "logtoctl %s: %s\n", cmd, msg)
		return ExitError
	}
	doc, err := load(*file, stdin)
	if err != nil {
		return fail(err)
	}
	if err := config.Validate(doc.Config); err != nil {
		return fail(err)
	}
	if cmd == "render" {
		return render(doc.Config, stdout, fail)
	}

	resolver := secrets.CLI(redactor)
	env, err := target.Connect(ctx, &doc.Config.Spec, resolver)
	if err != nil {
		return fail(err)
	}
	env.AllowUntested = *untested
	if env.Capability.Version == "" && *untested {
		_, _ = fmt.Fprintln(stderr, "logtoctl: warning: this Logto is outside the support window (--allow-untested-logto)")
	}
	e, err := engine.New(kinds.All()...)
	if err != nil {
		return fail(err)
	}
	switch cmd {
	case "plan", "check":
		p, err := e.Plan(ctx, env)
		if err != nil {
			return fail(err)
		}
		if err := plan.Render(stdout, p); err != nil {
			return fail(err)
		}
		if cmd == "check" && p.Changed() {
			_, _ = fmt.Fprintln(stderr, "logtoctl check: Logto differs from the document")
			return ExitDrift
		}
		return ExitOK
	default: // apply
		p, err := e.Apply(ctx, env)
		if p != nil {
			_ = plan.Render(stderr, p)
		}
		if err != nil {
			if p != nil {
				_, _ = fmt.Fprintln(stderr, "logtoctl apply: "+results(p))
			}
			if engine.Transient(err) {
				err = fmt.Errorf("%w (transient: a later apply may succeed)", err)
			}
			return fail(err)
		}
		if err := outputs.WriteIDs(stdout, env.Outputs, *format); err != nil {
			return fail(err)
		}
		return publishSecrets(env.Outputs, *secretsOut, *secretsFD, *printSecrets, stdout, stderr, fail)
	}
}

// load reads the document at path, or on stdin for "-" (its relative files then resolve against the working
// directory).
func load(path string, stdin io.Reader) (*config.Document, error) {
	if path != "-" {
		return config.Load(path)
	}
	raw, err := io.ReadAll(stdin)
	if err != nil {
		return nil, fmt.Errorf("<stdin>: %w", err)
	}
	return config.Read(raw, "<stdin>", ".")
}

// render prints the document as it will be applied: files inlined, variables replaced and dropped. Every ${ left
// in a value (written $${ in the source, or inside an inlined file) is written back as $${, so that reading the
// output again (logtoctl, or the operator on an object made of it) gives the same values.
func render(cfg *v1alpha1.LogtoConfig, stdout io.Writer, fail func(error) int) int {
	out := cfg.DeepCopy()
	out.Spec.Variables = nil
	js, err := json.Marshal(out)
	if err != nil {
		return fail(err)
	}
	var tree map[string]any
	if err := json.Unmarshal(js, &tree); err != nil {
		return fail(err)
	}
	delete(tree, "status")
	tree["spec"] = escape(tree["spec"])
	b, err := yaml.Marshal(tree)
	if err != nil {
		return fail(err)
	}
	_, _ = stdout.Write(b)
	return ExitOK
}

// escape writes every ${ of the strings under v as $${.
func escape(v any) any {
	switch x := v.(type) {
	case string:
		return strings.ReplaceAll(x, "${", "$${")
	case map[string]any:
		for k, e := range x {
			x[k] = escape(e)
		}
	case []any:
		for i, e := range x {
			x[i] = escape(e)
		}
	}
	return v
}

func publishSecrets(o *engine.Outputs, path string, fd int, print bool, stdout, stderr io.Writer, fail func(error) int) int {
	if len(o.Secrets) == 0 {
		return ExitOK
	}
	switch {
	case path != "":
		if err := outputs.WriteSecretsFile(o, path); err != nil {
			return fail(err)
		}
		_, _ = fmt.Fprintf(stderr, "logtoctl apply: %d secret(s) written to %s (mode 0600)\n", len(o.Secrets), path)
	case fd >= 0:
		if err := outputs.WriteSecretsFD(o, fd); err != nil {
			return fail(err)
		}
		_, _ = fmt.Fprintf(stderr, "logtoctl apply: %d secret(s) written to descriptor %d\n", len(o.Secrets), fd)
	case print:
		_, _ = fmt.Fprintln(stderr, "logtoctl apply: --print-secrets puts secrets on stdout; prefer --secrets-out or --secrets-fd")
		for _, k := range sortedKeys(o.Secrets) {
			_, _ = fmt.Fprintf(stdout, "%s=%s\n", k, o.Secrets[k].Reveal())
		}
	default:
		_, _ = fmt.Fprintf(stderr, "logtoctl apply: %d secret(s) published but not written: use --secrets-out or --secrets-fd\n", len(o.Secrets))
		return ExitWarnings
	}
	return ExitOK
}

// results counts what an interrupted apply did: the ops applied, the one that failed, the writes never run.
func results(p *plan.Plan) string {
	applied, notRun := 0, 0
	failed := ""
	for _, op := range p.Ops {
		switch {
		case op.Result == plan.Applied:
			applied++
		case op.Result == plan.Failed:
			failed = ", failed " + op.Kind + " " + op.Key
		case op.Action != plan.Noop:
			notRun++
		}
	}
	return fmt.Sprintf("stopped: %d applied%s, %d not run (the plan above lists them all)", applied, failed, notRun)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Main is the binary's entry point.
func Main(version string) {
	os.Exit(Run(context.Background(), os.Args[1:], version, os.Stdin, os.Stdout, os.Stderr))
}
