// Package api turns service results into HTTP statuses.
package api

import (
	"net/http"
	"strings"
)

// Status is the HTTP status for an operation's error.
func Status(err error) int {
	switch {
	case err == nil:
		return http.StatusOK
	case strings.Contains(err.Error(), "not found"):
		return http.StatusNotFound
	case strings.Contains(err.Error(), "conflict"):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}
