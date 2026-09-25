package app

import (
	"os"
	"strings"

	"github.com/viktordanov/uagent-harness/internal/home"
	"github.com/viktordanov/uagent-harness/internal/home/migrate"
)

// checkHome reports uah's home, whether it was copied from the old folders,
// and variables uah no longer reads.
func checkHome(getenv func(string) string) []Check {
	var checks []Check
	if lines := home.Warnings(getenv); len(lines) > 0 {
		checks = append(checks, warn("environment", strings.Join(lines, "; "), "rename them where they are set, such as your shell profile"))
	}
	dir := home.Dir()
	rec, migrated, err := migrate.ReadRecord(dir)
	if err != nil {
		return append(checks, warn("home", err.Error(), "remove "+dir+"/"+migrate.Marker+" if it is damaged"))
	}
	if migrated {
		from := strings.Join(nonEmpty(rec.Config, rec.State), " and ")
		if from == "" {
			from = "the old folders"
		}

		return append(checks, ok("home", dir+", copied from "+from+" on "+rec.At.Local().Format("2006-01-02")))
	}
	userHome, err := os.UserHomeDir()
	if getenv(home.Env) != "" || err != nil {
		return append(checks, ok("home", dir))
	}
	old := migrate.Old(getenv, userHome)
	if !old.HasOld() {
		return append(checks, ok("home", dir))
	}
	if _, err := os.Stat(dir); err != nil {
		return append(checks, fail("home", "copying "+old.Config+" and "+old.State+" to "+dir+" failed",
			"run uah again to retry; the error is printed when it starts"))
	}

	return append(checks, warn("home", dir+" was not copied from "+old.Config+" or "+old.State+"; uah reads only "+dir,
		"to copy them, move "+dir+" aside and run uah again"))
}

func nonEmpty(values ...string) []string {
	var out []string
	for _, v := range values {
		if v != "" {
			out = append(out, v)
		}
	}

	return out
}
