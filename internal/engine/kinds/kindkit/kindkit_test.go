package kindkit_test

import (
	"testing"

	"github.com/Aliud-Corp/logto-operator/internal/engine/kinds/kindkit"
)

func TestOverlay(t *testing.T) {
	for name, tc := range map[string]struct {
		have, want any
		out        string
	}{
		"declared keys over Logto's, the others kept": {
			map[string]any{"a": 1, "b": 2}, map[string]any{"a": 9}, `{"a":9,"b":2}`,
		},
		"objects merged recursively": {
			map[string]any{"x": map[string]any{"a": 1, "b": 2}}, map[string]any{"x": map[string]any{"b": 3}}, `{"x":{"a":1,"b":3}}`,
		},
		"a list is the declared one": {
			map[string]any{"l": []any{1, 2, 3}}, map[string]any{"l": []any{4}}, `{"l":[4]}`,
		},
		"an object over null": {
			map[string]any{"x": nil}, map[string]any{"x": map[string]any{"a": 1}}, `{"x":{"a":1}}`,
		},
		"a scalar over an object": {
			map[string]any{"x": map[string]any{"a": 1}}, map[string]any{"x": "s"}, `{"x":"s"}`,
		},
		"a declared null": {
			map[string]any{"x": "s"}, map[string]any{"x": nil}, `{"x":null}`,
		},
		"nothing held": {
			nil, map[string]any{"a": 1}, `{"a":1}`,
		},
		"a Go value, as it marshals": {
			map[string]any{"name": "old", "extra": true}, struct {
				Name string `json:"name"`
			}{"new"}, `{"extra":true,"name":"new"}`,
		},
	} {
		if got := kindJSON(kindkit.Overlay(tc.have, tc.want)); got != tc.out {
			t.Errorf("%s: %s, want %s", name, got, tc.out)
		}
	}
}

func TestInSyncIgnoresWhatLogtoHoldsBesideTheDeclaredValue(t *testing.T) {
	have := map[string]any{"logoUrl": "a", "darkLogoUrl": "b"}
	if !kindkit.InSync(have, map[string]any{"logoUrl": "a"}) {
		t.Error("an undeclared field Logto holds is a drift")
	}
	if kindkit.InSync(have, map[string]any{"logoUrl": "c"}) {
		t.Error("a changed declared field is in sync")
	}
	if kindkit.InSync(map[string]any{"l": []any{1, 2}}, map[string]any{"l": []any{1}}) {
		t.Error("a shorter declared list is in sync")
	}
}

func TestSubsetAndEqual(t *testing.T) {
	if !kindkit.Subset(map[string]any{"a": 1}, map[string]any{"a": 1.0, "b": 2}) {
		t.Error("a number decoded as float is not the same number")
	}
	if kindkit.Subset([]any{1}, []any{1, 2}) {
		t.Error("lists of different lengths are a subset")
	}
	if kindkit.Equal(map[string]any{"a": 1}, map[string]any{"a": 1, "b": 2}) {
		t.Error("equal ignores a key")
	}
	for _, pair := range [][2]any{{587, "587"}, {true, "true"}, {nil, "<nil>"}, {[]any{1}, "[1]"}} {
		if kindkit.Equal(pair[0], pair[1]) || kindkit.Subset(map[string]any{"v": pair[0]}, map[string]any{"v": pair[1]}) {
			t.Errorf("%#v and %#v compare equal: a type change would not show as drift", pair[0], pair[1])
		}
	}
}

func TestOverlayByMatchesListItemsByKey(t *testing.T) {
	have := []any{
		map[string]any{"identifier": "email", "password": true, "future": "kept"},
		map[string]any{"identifier": "username", "password": true},
	}
	want := []any{map[string]any{"identifier": "email", "password": false}, map[string]any{"identifier": "phone"}}
	got := kindJSON(kindkit.OverlayBy("identifier", have, want))
	if got != `[{"future":"kept","identifier":"email","password":false},{"identifier":"phone"}]` {
		t.Fatalf("%s", got)
	}
}

func kindJSON(v any) string { return kindkit.JSON(v) }
