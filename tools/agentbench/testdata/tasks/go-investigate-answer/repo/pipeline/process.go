package pipeline

// ProcessBatch runs a batch through every stage: normalize, validate,
// drop monitoring records, enrich, route, and group.
func ProcessBatch(in []Record) Result {
	recs := normalizeAll(in)
	recs, rejected := validateAll(recs)
	recs = withoutTestRecords(recs)
	recs = enrichAll(recs)
	recs = route(recs)

	return Result{Groups: bucketize(recs), Rejected: rejected}
}
