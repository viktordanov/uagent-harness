package handlers

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"example.com/bookmarks/internal/model"
)

// list serves GET /bookmarks. The optional q parameter keeps the bookmarks
// whose name or URL contains it, ignoring case; tag keeps the ones with that
// tag, ignoring case. limit and offset page through what is left.
func (a *API) list(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	q := strings.TrimSpace(query.Get("q"))
	tag := strings.TrimSpace(query.Get("tag"))
	limit, err := intParam(query.Get("limit"), -1)
	if err != nil {
		writeError(w, http.StatusBadRequest, "limit: "+err.Error())
		return
	}
	offset, err := intParam(query.Get("offset"), 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, "offset: "+err.Error())
		return
	}
	out := make([]model.Bookmark, 0)
	for _, b := range a.store.List() {
		if b.Matches(q) && (tag == "" || hasTag(b, tag)) {
			out = append(out, b)
		}
	}
	if offset >= len(out) {
		out = out[:0]
	} else {
		out = out[offset:]
	}
	if limit >= 0 && limit < len(out) {
		out = out[:limit]
	}
	writeJSON(w, http.StatusOK, out)
}

// intParam parses a non-negative integer query parameter; an empty value
// gives def.
func intParam(v string, def int) (int, error) {
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("%q is not a non-negative integer", v)
	}
	return n, nil
}

func hasTag(b model.Bookmark, tag string) bool {
	for _, t := range b.Tags {
		if strings.EqualFold(t, tag) {
			return true
		}
	}
	return false
}

// delete serves DELETE /bookmarks/{id}.
func (a *API) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := a.store.Delete(id); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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

// update serves PUT /bookmarks/{id}. It replaces the URL, name and tags.
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
