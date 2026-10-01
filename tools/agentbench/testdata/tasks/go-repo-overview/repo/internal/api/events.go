package api

import (
	"encoding/json"
	"net/http"

	"example.com/shipd/internal/queue"
	"example.com/shipd/internal/store"
)

type eventRequest struct {
	ShipmentID string `json:"shipment_id"`
	Status     string `json:"status"`
	Location   string `json:"location"`
}

// handleCreateEvent stores a shipment event and enqueues one notification
// per subscriber. A repeated Idempotency-Key returns the first response
// without storing the event again.
func (s *Server) handleCreateEvent(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Idempotency-Key")
	if key != "" && s.seen.Seen(key) {
		w.WriteHeader(http.StatusOK)

		return
	}
	var req eventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad json")

		return
	}
	if err := validate(req); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())

		return
	}
	ev := store.Event{ShipmentID: req.ShipmentID, Status: req.Status, Location: req.Location}
	saved, err := s.store.AppendEvent(r.Context(), ev)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store failed")

		return
	}
	subs, err := s.store.Subscribers(r.Context(), req.ShipmentID)
	if err == nil {
		for _, sub := range subs {
			s.jobs.Enqueue(queue.Job{URL: sub.URL, Secret: sub.Secret, Event: saved})
		}
	}
	if key != "" {
		s.seen.Remember(key)
	}
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(saved)
}

func (s *Server) handleGetShipment(w http.ResponseWriter, r *http.Request) {
	sh, err := s.store.Shipment(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "no such shipment")

		return
	}
	_ = json.NewEncoder(w).Encode(sh)
}
