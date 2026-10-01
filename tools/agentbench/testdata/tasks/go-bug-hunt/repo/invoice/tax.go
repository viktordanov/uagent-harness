package invoice

// Region tax rates in basis points (825 is 8.25%).
var taxRates = map[string]int64{
	"CA": 725,
	"NY": 400,
	"TX": 625,
	"OR": 0,
}

// tax returns the tax on an amount for a region, rounded half up.
func tax(amount Cents, region string) Cents {
	return mulRate(amount, taxRates[region])
}
