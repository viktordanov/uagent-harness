package model

import "sort"

// SortByName sorts Account values by name, then ID.
func SortByName(us []Account) {
	sort.Slice(us, func(i, j int) bool {
		if us[i].Name != us[j].Name {
			return us[i].Name < us[j].Name
		}

		return us[i].ID < us[j].ID
	})
}
