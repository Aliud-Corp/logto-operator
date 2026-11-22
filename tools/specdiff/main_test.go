package main

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func doc(t *testing.T, s string) map[string]any {
	t.Helper()
	var d map[string]any
	if err := json.Unmarshal([]byte(s), &d); err != nil {
		t.Fatal(err)
	}
	return d
}

const before = `{"paths":{"/api/things":{"get":{"operationId":"ListThings","responses":{"200":{"content":{"application/json":{"schema":{"type":"array","items":{"type":"object","required":["id"],"properties":{"id":{"type":"string"},"name":{"type":"string"}}}}}}}}}}}}`

const nameRemoved = `{"paths":{"/api/things":{"get":{"operationId":"ListThings","responses":{"200":{"content":{"application/json":{"schema":{"type":"array","items":{"type":"object","required":["id"],"properties":{"id":{"type":"string"},"color":{"type":"string"}}}}}}}}}}}}`

func TestARemovedPropertyOfAUsedOperationFails(t *testing.T) {
	findings := Diff(doc(t, before), doc(t, nameRemoved))
	var out bytes.Buffer
	if code := report(findings, map[string]bool{"ListThings": true}, &out); code != 1 {
		t.Fatalf("exit %d, want 1:\n%s", code, out.String())
	}
	if !strings.Contains(out.String(), "GET /api/things response 200[].name: property removed  [USED]") {
		t.Fatalf("the removed property is not named:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "GET /api/things response 200[].color: property added") {
		t.Fatalf("the added property is not reported:\n%s", out.String())
	}
}

func TestTheSameChangeOnAnUnusedOperationDoesNotFail(t *testing.T) {
	var out bytes.Buffer
	if code := report(Diff(doc(t, before), doc(t, nameRemoved)), map[string]bool{}, &out); code != 0 {
		t.Fatalf("exit %d, want 0:\n%s", code, out.String())
	}
}

func TestANewRequiredRequestPropertyIsBreaking(t *testing.T) {
	a := doc(t, `{"paths":{"/x":{"post":{"operationId":"Make","requestBody":{"content":{"application/json":{"schema":{"type":"object","properties":{"a":{"type":"string"}}}}}}}}}}`)
	b := doc(t, `{"paths":{"/x":{"post":{"operationId":"Make","requestBody":{"content":{"application/json":{"schema":{"type":"object","required":["b"],"properties":{"a":{"type":"string"},"b":{"type":"string"}}}}}}}}}}`)
	f := Diff(a, b)
	if len(f) != 1 || f[0].Level != Breaking || !strings.Contains(f[0].String(), "b: new required request property") {
		t.Fatalf("findings = %v", f)
	}
}

// The measured result of 2026-11-21 on the vendored window: 1.44.0 to 1.46.0 breaks 7 operations, none of them used.
func TestTheVendoredWindowDiffMatchesTheMeasurement(t *testing.T) {
	from, err := load("../../api/logto/1.44.0.openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	to, err := load("../../api/logto/1.46.0.openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	counts := map[Level]int{}
	for _, f := range Diff(from, to) {
		counts[f.Level]++
	}
	if counts[Breaking] != 7 || counts[Changed] != 3 || counts[Additive] != 82 {
		t.Fatalf("counts = breaking %d, changed %d, additive %d; want 7, 3, 82", counts[Breaking], counts[Changed], counts[Additive])
	}
}

// The code calls an operation by the name the older spec gives it (the generated mgmt.New<OperationId>Request): a
// rename is breaking for it, and every other finding of that operation stays under that name.
func TestARenamedUsedOperationIsBreakingAndKeepsItsFindingsUnderTheUsedName(t *testing.T) {
	renamed := strings.Replace(nameRemoved, `"operationId":"ListThings"`, `"operationId":"ListAllThings"`, 1)
	var out bytes.Buffer
	if code := report(Diff(doc(t, before), doc(t, renamed)), map[string]bool{"ListThings": true}, &out); code != 1 {
		t.Fatalf("exit %d, want 1:\n%s", code, out.String())
	}
	for _, want := range []string{"operationId ListThings -> ListAllThings", "200[].name: property removed  [USED]"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("%q not reported:\n%s", want, out.String())
		}
	}
}

// used.txt is the list specdiff guards: it must be exactly the operations the code calls.
func TestUsedListsExactlyTheOperationsTheCodeCalls(t *testing.T) {
	call := regexp.MustCompile(`mgmt\.New([A-Za-z0-9]+?)Request(?:WithBody)?\(`)
	inCode := map[string]bool{}
	for _, root := range []string{"../../internal", "../../cmd"} {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || strings.Contains(p, "/mgmt/") {
				return err
			}
			b, err := os.ReadFile(p)
			for _, m := range call.FindAllSubmatch(b, -1) {
				inCode[string(m[1])] = true
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	listed, err := loadUsed("used.txt")
	if err != nil {
		t.Fatal(err)
	}
	for op := range inCode {
		if !listed[op] {
			t.Errorf("used.txt misses %s, which the code calls", op)
		}
	}
	for op := range listed {
		if !inCode[op] {
			t.Errorf("used.txt lists %s, which no code calls", op)
		}
	}
}
