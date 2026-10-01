package pipeline

// regions are the known region codes and their shard.
var regions = map[string]int{"eu": 0, "na": 1, "ap": 2, "latam": 3}

func knownRegion(r string) bool {
	_, ok := regions[r]

	return ok
}

// unassignedShard is where records without a region are routed.
const unassignedShard = 4
