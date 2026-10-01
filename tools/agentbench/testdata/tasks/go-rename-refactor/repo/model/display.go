package model

import "fmt"

// Display is how a UserRecord appears in lists.
func (u UserRecord) Display() string {
	if u.Admin {
		return fmt.Sprintf("%s <%s> (admin)", u.Name, u.Email)
	}

	return fmt.Sprintf("%s <%s>", u.Name, u.Email)
}
