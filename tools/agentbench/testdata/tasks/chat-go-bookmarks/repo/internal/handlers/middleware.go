package handlers

import (
	"log"
	"net/http"
	"time"
)

// Recover turns a panic in h into a 500 with the usual JSON error body.
func Recover(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				log.Printf("bookmarks: panic serving %s %s: %v", r.Method, r.URL.Path, v)
				writeError(w, http.StatusInternalServerError, "internal error")
			}
		}()
		h.ServeHTTP(w, r)
	})
}

// statusRecorder remembers the status code written through it.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Logging logs one line per request: method, path, status and duration.
func Logging(h http.Handler, logger *log.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		h.ServeHTTP(rec, r)
		logger.Printf("%s %s %d %s", r.Method, r.URL.RequestURI(), rec.status, time.Since(start).Round(time.Microsecond))
	})
}
