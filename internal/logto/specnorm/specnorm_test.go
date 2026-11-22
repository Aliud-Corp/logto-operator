package specnorm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeRewritesALiteralFormatIntoAnEnum(t *testing.T) {
	in := []byte(`{"a":{"type":"string","format":"\"Social\""},"b":{"type":"string","format":"url"},"n":12345678901234567890}`)

	out, n, err := Normalize(in, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("rewrote %d schemas, want 1", n)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	a, _ := doc["a"].(map[string]any)
	b, _ := doc["b"].(map[string]any)
	if _, has := a["format"]; has {
		t.Errorf("literal format kept: %v", a)
	}
	if enum, _ := a["enum"].([]any); len(enum) != 1 || enum[0] != "Social" {
		t.Errorf("enum = %v, want [Social]", a["enum"])
	}
	if b["format"] != "url" {
		t.Errorf("a real format was touched: %v", b)
	}
	if !json.Valid(out) || !strings.Contains(string(out), "12345678901234567890") {
		t.Errorf("a number lost its precision: %s", out)
	}
}

func TestNormalizeRefusesAQuotedFormatThatIsNotJSON(t *testing.T) {
	if _, _, err := Normalize([]byte(`{"format":"\"broken"}`), nil); err == nil {
		t.Fatal("want an error")
	}
}

func TestAnOverlayWhoseTargetIsGoneFails(t *testing.T) {
	doc := map[string]any{"a": map[string]any{"type": "string"}}
	err := applyOverlays(doc, []Overlay{{Pointer: "/a", Expect: []string{"enum"}, Key: "x", Value: 1}})
	if err == nil {
		t.Fatal("want an error: the target lost the key the overlay was written for")
	}
	if err := applyOverlays(doc, []Overlay{{Pointer: "/b", Key: "x", Value: 1}}); err == nil {
		t.Fatal("want an error: the target does not exist")
	}
}

func TestAnOverlaySetsItsKey(t *testing.T) {
	doc := map[string]any{"p": []any{map[string]any{"enum": []any{"A"}}}}
	if err := applyOverlays(doc, []Overlay{{Pointer: "/p/0", Expect: []string{"enum"}, Key: "x-go-type-name", Value: "T"}}); err != nil {
		t.Fatal(err)
	}
	if got := doc["p"].([]any)[0].(map[string]any)["x-go-type-name"]; got != "T" {
		t.Fatalf("x-go-type-name = %v", got)
	}
}

// The real inputs: every vendored spec of the support window must normalize with the real overlays. A Logto upgrade
// that moves an overlay target fails here, before the generator runs.
func TestEveryVendoredSpecNormalizes(t *testing.T) {
	files, err := filepath.Glob("../../../api/logto/*.openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no vendored spec found under api/logto/")
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			_, n, err := Normalize(raw, Overlays)
			if err != nil {
				t.Fatal(err)
			}
			if n == 0 {
				t.Fatal("no literal format rewritten: the normalization step may no longer be needed")
			}
		})
	}
}
