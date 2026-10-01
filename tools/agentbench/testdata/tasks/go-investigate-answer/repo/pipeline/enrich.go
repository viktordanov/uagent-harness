package pipeline

// taxRates are basis points per region; unassigned records pay the
// default rate.
var taxRates = map[string]int64{"eu": 2000, "na": 800, "ap": 1000, "latam": 1500}

const defaultTaxRate = 1000

// lookupTaxRate returns the region's rate, or the default for an empty or
// unknown region.
func lookupTaxRate(region string) int64 {
	if rate, ok := taxRates[region]; ok {
		return rate
	}

	return defaultTaxRate
}

func enrichAll(in []Record) []Record {
	for i := range in {
		in[i].Tax = in[i].Amount * lookupTaxRate(in[i].Region) / 10000
	}

	return in
}
