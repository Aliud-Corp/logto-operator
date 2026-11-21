package logto_test

import (
	"bytes"
	"testing"

	logtospecs "github.com/Aliud-Corp/logto-operator/api/logto"
)

func TestEachVendoredSpecIsRecognisedWhateverItsServers(t *testing.T) {
	for _, v := range logtospecs.Versions() {
		raw, _ := logtospecs.Spec(v)
		served := bytes.Replace(raw, []byte(`"servers":[`), []byte(`"servers":[{"url":"https://auth.example.test"},`), 1)
		if got, err := logtospecs.Match(served); err != nil || got != v {
			t.Errorf("%s recognised as %q (%v)", v, got, err)
		}
	}
}

func TestAnUntestedSpecIsRecognisedAsNone(t *testing.T) {
	raw, _ := logtospecs.Spec(logtospecs.Versions()[0])
	changed := bytes.Replace(raw, []byte(`"paths":{`), []byte(`"paths":{"/api/new-in-a-later-logto":{},`), 1)
	if bytes.Equal(changed, raw) {
		t.Fatal("the test spec was not changed")
	}
	if got, err := logtospecs.Match(changed); err != nil || got != "" {
		t.Fatalf("an untested spec recognised as %q (%v)", got, err)
	}
}
