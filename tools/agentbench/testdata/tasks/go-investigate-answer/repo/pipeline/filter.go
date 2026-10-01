package pipeline

// withoutTestRecords drops synthetic monitoring records.
func withoutTestRecords(in []Record) []Record {
	out := in[:0:0]
	for _, r := range in {
		if r.Test {
			continue
		}
		out = append(out, r)
	}

	return out
}
