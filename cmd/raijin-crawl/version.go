package main

import (
	"runtime/debug"
	"sort"
	"strings"

	raijincrawl "github.com/protonnegativo/raijin-crawl"
)

// releaseVersion is set at link time by release builds: -ldflags "-X main.releaseVersion=v1.2.3".
// It must stay a plain string variable without an initializer, or -X ignores it.
var releaseVersion string

// version is releaseVersion when a release build set it, else the module version the binary was installed
// with (go install ...@v1.2.3 or @latest), or "dev" with the VCS revision for a local go build, e.g.
// "dev-0123456789ab+dirty".
func version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		info = &debug.BuildInfo{}
	}
	return buildVersion(info)
}

func buildVersion(info *debug.BuildInfo) string {
	if releaseVersion != "" {
		return releaseVersion
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	var rev, dirty string
	for _, s := range info.Settings {
		switch {
		case s.Key == "vcs.revision":
			rev = s.Value
		case s.Key == "vcs.modified" && s.Value == "true":
			dirty = "+dirty"
		}
	}
	if rev == "" {
		return "dev" + dirty
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	return "dev-" + rev + dirty
}

// versionLine is what --version prints: the tool version and the engine versions it was tested with.
func versionLine(v string) string {
	line := "raijin-crawl " + v
	e, err := raijincrawl.Engines()
	if err != nil || len(e) == 0 {
		return line
	}
	var tested []string
	for name, eng := range e {
		tested = append(tested, name+" "+eng.Version)
	}
	sort.Strings(tested)
	return line + " (tested with " + strings.Join(tested, ", ") + ")"
}
