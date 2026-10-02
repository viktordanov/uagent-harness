package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"example.com/bookmarks/internal/model"
)

// maxBody is the largest request body accepted, in bytes.
const maxBody = 64 << 10

// bookmarkRequest is the body of POST /bookmarks and PUT /bookmarks/{id}.
// Title is the old name of Name, still accepted from older clients; Name
// wins when both are set.
type bookmarkRequest struct {
	URL   string   `json:"url"`
	Name  string   `json:"name"`
	Title string   `json:"title"`
	Tags  []string `json:"tags"`
}

func (req bookmarkRequest) bookmark() model.Bookmark {
	name := req.Name
	if name == "" {
		name = req.Title
	}
	return model.Bookmark{URL: req.URL, Name: name, Tags: req.Tags}
}

// decodeBookmarkRequest reads one JSON object from the body. Unknown fields
// and trailing data are errors, so a client's typo is not silently dropped.
func decodeBookmarkRequest(w http.ResponseWriter, r *http.Request) (bookmarkRequest, error) {
	var req bookmarkRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		if errors.Is(err, io.EOF) {
			return req, errors.New("request body is empty")
		}
		return req, fmt.Errorf("bad request body: %v", err)
	}
	if dec.More() {
		return req, errors.New("bad request body: more than one JSON value")
	}
	return req, nil
}
