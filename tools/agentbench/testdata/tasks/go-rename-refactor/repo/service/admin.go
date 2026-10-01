package service

import "example.com/accounts/model"

// Promote makes the UserRecord with the ID an admin.
func (s *Service) Promote(id int) (model.UserRecord, bool) {
	u, ok := s.st.Get(id)
	if !ok {
		return model.UserRecord{}, false
	}
	u.Admin = true
	s.st.Put(u)

	return u, true
}
