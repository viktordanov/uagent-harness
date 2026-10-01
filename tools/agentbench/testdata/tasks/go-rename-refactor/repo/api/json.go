// Package api renders users for HTTP clients.
package api

import (
	"encoding/json"

	"example.com/accounts/model"
)

// userJSON is the wire form of a model.UserRecord.
type userJSON struct {
	ID    int    `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Admin bool   `json:"admin,omitempty"`
}

func toJSON(u model.UserRecord) userJSON {
	return userJSON{ID: u.ID, Email: u.Email, Name: u.Name, Admin: u.Admin}
}

// Encode renders one UserRecord.
func Encode(u model.UserRecord) ([]byte, error) { return json.Marshal(toJSON(u)) }

// EncodeList renders a list of UserRecord values.
func EncodeList(us []model.UserRecord) ([]byte, error) {
	out := make([]userJSON, len(us))
	for i, u := range us {
		out[i] = toJSON(u)
	}

	return json.Marshal(out)
}
