package handlers

import (
	"net/http"
	"strings"

	"example.com/bookmarks/internal/model"
)

// list serves GET /bookmarks. The optional q parameter keeps the bookmarks
// whose title or URL contains it, ignoring case.
func (a *API) list(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	out := make([]model.Bookmark, 0)
	for _, b := range a.store.List() {
		if b.Matches(q) {
			out = append(out, b)
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// get serves GET /bookmarks/{id}.
func (a *API) get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	b, err := a.store.Get(id)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

// create serves POST /bookmarks.
func (a *API) create(w http.ResponseWriter, r *http.Request) {
	req, err := decodeBookmarkRequest(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	b, err := a.store.Create(req.bookmark())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	w.Header().Set("Location", "/bookmarks/"+formatID(b.ID))
	writeJSON(w, http.StatusCreated, b)
}

// update serves PUT /bookmarks/{id}. It replaces the URL, title and tags.
func (a *API) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	req, err := decodeBookmarkRequest(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	b, err := a.store.Update(id, req.bookmark())
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}
