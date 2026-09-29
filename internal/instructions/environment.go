package instructions

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// EnvironmentOpen starts the environment block, for /context.
const EnvironmentOpen = "<environment_context>"

// Environment is what Codex's <environment_context> tells the model about
// the machine for one local environment (codex-rs/core/src/context/
// world_state/environment.rs at rust-v0.156.1): the working directory,
// the shell's name, the local date, and the IANA time zone.
type Environment struct {
	Cwd         string
	Shell       string
	CurrentDate string
	Timezone    string
}

// LocalEnvironment is the environment of a session in workspace whose
// commands run in shell (a path; the block names its base name), at now.
// As Codex does, the date is local when the time zone is known, and the
// UTC date in Etc/UTC otherwise.
func LocalEnvironment(workspace, shell string, now time.Time, getenv func(string) string) Environment {
	tz, loc := localZone(getenv, os.Readlink)
	if loc == nil {
		tz, loc = "Etc/UTC", time.UTC
	}

	return Environment{Cwd: workspace, Shell: filepath.Base(shell), CurrentDate: now.In(loc).Format(time.DateOnly), Timezone: tz}
}

// localZone is the IANA name of the local time zone, as the TZ variable
// or the /etc/localtime link names it, and the zone itself; nil when
// neither names a zone Go can load.
func localZone(getenv func(string) string, readlink func(string) (string, error)) (string, *time.Location) {
	name := getenv("TZ")
	if name == "" {
		link, err := readlink("/etc/localtime")
		if err != nil {
			return "", nil
		}
		name = link
	}
	name = strings.TrimPrefix(name, ":")
	if _, rest, ok := strings.Cut(name, "zoneinfo/"); ok {
		name = rest
	}
	if name == "" || strings.HasPrefix(name, "/") {
		return "", nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return "", nil
	}

	return name, loc
}

// String renders the block as Codex's legacy single environment does:
// each value on its own line, XML-escaped, and nothing for an empty one.
func (e Environment) String() string {
	var b strings.Builder
	b.WriteString(EnvironmentOpen + "\n")
	for _, f := range [][2]string{{"cwd", e.Cwd}, {"shell", e.Shell}, {"current_date", e.CurrentDate}, {"timezone", e.Timezone}} {
		if f[1] != "" {
			b.WriteString("  <" + f[0] + ">" + escapeXML(f[1]) + "</" + f[0] + ">\n")
		}
	}
	b.WriteString("</environment_context>")

	return b.String()
}

// escapeXML escapes text as Codex's push_xml_escaped_text does.
func escapeXML(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;").Replace(s)
}
