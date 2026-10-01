// Package api turns service results into HTTP statuses.
package api

import (
	"errors"
	"net/http"

	"example.com/kv/store"
)

// Status is the HTTP status for an operation's error.
func Status(err error) int {
	switch {
	case err == nil:
		return http.StatusOK
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, store.ErrConflict):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}
