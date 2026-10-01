package api

import (
	"encoding/json"
	"net/http"

	"example.com/shipd/internal/store"
)

func (s *Server) handleSubscribe(w http.ResponseWriter, r *http.Request) {
	var sub store.Subscriber
	if err := json.NewDecoder(r.Body).Decode(&sub); err != nil || sub.URL == "" || sub.ShipmentID == "" {
		writeError(w, http.StatusBadRequest, "need shipment_id and url")

		return
	}
	if err := s.store.AddSubscriber(r.Context(), sub); err != nil {
		writeError(w, http.StatusInternalServerError, "store failed")

		return
	}
	w.WriteHeader(http.StatusCreated)
}
