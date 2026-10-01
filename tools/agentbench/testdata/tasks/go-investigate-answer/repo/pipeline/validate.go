package pipeline

// validateRecord explains why a record is invalid, or returns "". Region is
// optional and not checked here.
func validateRecord(r Record) string {
	switch {
	case r.ID == "":
		return "missing id"
	case r.Amount < 0:
		return "negative amount"
	case r.Region != "" && !knownRegion(r.Region):
		return "unknown region " + r.Region
	}

	return ""
}

func validateAll(in []Record) ([]Record, []Rejection) {
	var ok []Record
	var bad []Rejection
	for _, r := range in {
		if why := validateRecord(r); why != "" {
			bad = append(bad, Rejection{r.ID, why})

			continue
		}
		ok = append(ok, r)
	}

	return ok, bad
}
