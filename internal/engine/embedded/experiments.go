package embedded

// experimentsEnv names the experiments a process runs, comma-separated.
// An experiment is a switch for an A/B benchmark, not a setting: it has no
// config key, and this is the one place that reads it.
const experimentsEnv = "UAH_EXPERIMENTS"

// experiments are the switches UAH_EXPERIMENTS turns on.
type experiments struct{}

// readExperiments reads UAH_EXPERIMENTS through getenv. No experiment runs
// now, so it turns nothing on.
func readExperiments(getenv func(string) string) experiments { //nolint:unparam // the next experiment reads it
	_ = getenv(experimentsEnv)

	return experiments{}
}
