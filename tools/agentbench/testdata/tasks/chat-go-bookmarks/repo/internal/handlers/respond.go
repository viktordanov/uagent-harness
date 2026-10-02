package handlers

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"

	"example.com/bookmarks/internal/model"
	"example.com/bookmarks/internal/store"
)

// errorBody is the JSON body of every error response.
type errorBody struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("bookmarks: encode response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorBody{Error: msg})
}

// writeStoreError maps an error from the store to a status code.
func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrDuplicateURL):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, model.ErrInvalid):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		log.Printf("bookmarks: %v", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

// pathID reads the {id} path value. On a bad id it writes a 400 and
// returns false.
func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		writeError(w, http.StatusBadRequest, "id must be a positive integer")
		return 0, false
	}
	return id, true
}

func formatID(id int64) string { return strconv.FormatInt(id, 10) }
