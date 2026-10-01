// Package api renders users for HTTP clients.
package api

import (
	"encoding/json"

	"example.com/accounts/model"
)

// userJSON is the wire form of a model.Account.
type userJSON struct {
	ID    int    `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
	Admin bool   `json:"admin,omitempty"`
}

func toJSON(u model.Account) userJSON {
	return userJSON{ID: u.ID, Email: u.Email, Name: u.Name, Admin: u.Admin}
}

// Encode renders one Account.
func Encode(u model.Account) ([]byte, error) { return json.Marshal(toJSON(u)) }

// EncodeList renders a list of Account values.
func EncodeList(us []model.Account) ([]byte, error) {
	out := make([]userJSON, len(us))
	for i, u := range us {
		out[i] = toJSON(u)
	}

	return json.Marshal(out)
}
