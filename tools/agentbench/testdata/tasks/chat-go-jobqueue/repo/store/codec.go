package store

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"example.com/jobq/queue"
)

// formatVersion is the version of the file layout that encode writes. decode
// reads it and every older version.
const formatVersion = 1

// fileData is the layout of a store file:
//
//	{
//	  "version": 1,
//	  "next_id": 3,
//	  "jobs": [ {"id": "job-0001", "kind": "echo", "state": "done", ...}, ... ]
//	}
//
// next_id is the sequence number of the last ID handed out, so an ID is never
// reused after its job is purged.
type fileData struct {
	Version int          `json:"version"`
	NextID  int          `json:"next_id"`
	Jobs    []queue.Task `json:"jobs"`
}

// encode writes the store's contents as indented JSON.
func encode(w io.Writer, next int, tasks []queue.Task) error {
	if tasks == nil {
		tasks = []queue.Task{}
	}
	data := fileData{Version: formatVersion, NextID: next, Jobs: tasks}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		return fmt.Errorf("encode store: %w", err)
	}
	return nil
}

// decode reads a store file. An empty file is an empty store. Unknown fields
// are an error, so a typo in a hand-edited file is not silently dropped.
func decode(b []byte) (int, []queue.Task, error) {
	if len(bytes.TrimSpace(b)) == 0 {
		return 0, nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var data fileData
	if err := dec.Decode(&data); err != nil {
		return 0, nil, fmt.Errorf("decode store: %w", err)
	}
	if data.Version > formatVersion {
		return 0, nil, fmt.Errorf("decode store: version %d is newer than this jobq (%d)", data.Version, formatVersion)
	}
	if data.NextID < 0 {
		return 0, nil, fmt.Errorf("decode store: negative next_id %d", data.NextID)
	}
	return data.NextID, data.Jobs, nil
}
