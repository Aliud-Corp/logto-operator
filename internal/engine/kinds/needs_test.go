package kinds_test

import (
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/config"
	"github.com/Aliud-Corp/logto-operator/internal/engine"
	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds"
)

// Every write a kind can make is in its Needs, so that the capability check refuses, before any write, a Logto that
// lacks one (with --allow-untested-logto, the contract tests do not cover the target): the reference document, every
// prune switch on.
func TestEveryWriteAKindMakesIsInItsNeeds(t *testing.T) {
	doc, err := config.Load("../../../examples/reference.yaml")
	if err != nil {
		t.Fatal(err)
	}
	spec := &doc.Config.Spec
	p := &spec.Prune
	p.Connectors, p.CustomProfileFields, p.Resources, p.OrganizationScopes, p.OrganizationRoles, p.Roles = true, true, true, true, true, true
	env := &engine.Env{Spec: spec}

	needs := map[string]map[string]bool{} // package dir -> operations
	for _, k := range kinds.All() {
		dir := path.Base(reflect.TypeOf(k).PkgPath())
		if needs[dir] == nil {
			needs[dir] = map[string]bool{}
		}
		list, err := k.Needs(env)
		if err != nil {
			t.Fatalf("%s: %v", k.Name(), err)
		}
		for _, n := range list {
			needs[dir][n.OperationID] = true
		}
	}
	write := regexp.MustCompile(`mgmt\.New((?:Create|Update|Delete|Replace|Assign)[A-Za-z]*?)Request(?:WithBody)?\(`)
	for dir, have := range needs {
		files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range write.FindAllSubmatch(b, -1) {
				if op := string(m[1]); !have[op] {
					t.Errorf("%s writes with %s, absent from its Needs", dir, op)
				}
			}
		}
	}
}
