package pipeline

import "sort"

// shardFor is the shard of a region; unassigned records get their own.
func shardFor(region string) int {
	if s, ok := regions[region]; ok {
		return s
	}

	return unassignedShard
}

// route sets each record's shard and orders records by shard, then ID.
func route(in []Record) []Record {
	for i := range in {
		in[i].Shard = shardFor(in[i].Region)
	}
	sort.SliceStable(in, func(i, j int) bool {
		if in[i].Shard != in[j].Shard {
			return in[i].Shard < in[j].Shard
		}

		return in[i].ID < in[j].ID
	})

	return in
}
