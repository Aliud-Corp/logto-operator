package logtofake

import (
	"encoding/json"
	"net/http"
	"sort"
	"time"
)

// object is a stored Logto object, as Logto returns it (every field its spec requires).
type object = map[string]any

func decode(w http.ResponseWriter, r *http.Request) (object, bool) {
	var body object
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "guard.invalid_input", "message": err.Error()})
		return nil, false
	}
	return body, true
}

func str(o object, k string) string {
	s, _ := o[k].(string)
	return s
}

func stringList(o object, k string) []string {
	var out []string
	if l, ok := o[k].([]any); ok {
		for _, v := range l {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// merge copies the keys of patch into o (a PATCH).
func merge(o, patch object) {
	for k, v := range patch {
		o[k] = v
	}
}

func notFound(w http.ResponseWriter) {
	writeJSON(w, http.StatusNotFound, map[string]string{"code": "entity.not_exists", "message": "The entity does not exist."})
}

func conflict(w http.ResponseWriter, code string) {
	writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"code": code, "message": "Already exists."})
}

func now() float64 { return float64(time.Now().UnixMilli()) }

// sorted returns objects ordered by id, for stable lists.
func sorted(m map[string]object) []object {
	out := make([]object, 0, len(m))
	for _, o := range m {
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool { return str(out[i], "id") < str(out[j], "id") })
	return out
}

func copyObject(o object) object {
	c := object{}
	for k, v := range o {
		c[k] = v
	}
	return c
}
