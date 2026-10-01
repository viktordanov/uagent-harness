package pipeline

// groupKey is the key a record is grouped under.
func groupKey(r Record) string { return r.Region }

// bucketize groups routed records by region. Only regions with an index
// entry get a bucket, so each output group lines up with a downstream
// writer.
func bucketize(in []Record) map[string][]Record {
	index := map[string]int{}
	for region := range regions {
		index[region] = len(index)
	}
	out := map[string][]Record{}
	for _, r := range in {
		k := groupKey(r)
		if _, ok := index[k]; !ok {
			continue
		}
		out[k] = append(out[k], r)
	}

	return out
}
