package logtofake

import "net/http"

// table is the shape of most Management API collections: objects under an id Logto generates, one field kept
// unique (a name), 422 on its reuse, 404 on an unknown id. Its methods expect s.mu held, and those taking a
// ResponseWriter answer the error themselves when they return false.
type table struct {
	prefix string // of the generated ids
	unique string // the field Logto refuses twice; none when empty
	rows   map[string]object
}

func newTable(prefix, unique string) *table {
	return &table{prefix: prefix, unique: unique, rows: map[string]object{}}
}

// list is every object, by id.
func (t *table) list() []object { return sorted(t.rows) }

// insert stores o under a new id (set on o), unless its unique field is in use.
func (t *table) insert(s *Server, w http.ResponseWriter, o object) bool {
	if t.unique != "" {
		for _, row := range t.rows {
			if str(row, t.unique) == str(o, t.unique) {
				conflict(w, "entity.unique_integrity_violation")
				return false
			}
		}
	}
	id := s.nextID(t.prefix)
	o["id"] = id
	t.rows[id] = o
	return true
}

// get is the object of an id.
func (t *table) get(w http.ResponseWriter, id string) (object, bool) {
	o, ok := t.rows[id]
	if !ok {
		notFound(w)
	}
	return o, ok
}

// patch merges body into the object of an id.
func (t *table) patch(w http.ResponseWriter, id string, body object) (object, bool) {
	o, ok := t.get(w, id)
	if ok {
		merge(o, body)
	}
	return o, ok
}

// remove deletes the object of an id.
func (t *table) remove(w http.ResponseWriter, id string) bool {
	if _, ok := t.get(w, id); !ok {
		return false
	}
	delete(t.rows, id)
	return true
}
