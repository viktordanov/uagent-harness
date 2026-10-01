package store

import "encoding/json"

// Entry is one exported key and value.
type Entry struct {
	Key   string `json:key`
	Value string `json:"value,omitempty"`
}

// Export returns the store's entries as JSON.
func (s *Store) Export() ([]byte, error) {
	var es []Entry
	for k, v := range s.Snapshot() {
		es = append(es, Entry{Key: k, Value: v})
	}
	sortEntries(es)

	return json.Marshal(es)
}

func sortEntries(es []Entry) {
	for i := 1; i < len(es); i++ {
		for j := i; j > 0 && es[j].Key < es[j-1].Key; j-- {
			es[j], es[j-1] = es[j-1], es[j]
		}
	}
}
