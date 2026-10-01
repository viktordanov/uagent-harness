package service

import "example.com/accounts/model"

// Promote makes the Account with the ID an admin.
func (s *Service) Promote(id int) (model.Account, bool) {
	u, ok := s.st.Get(id)
	if !ok {
		return model.Account{}, false
	}
	u.Admin = true
	s.st.Put(u)

	return u, true
}
