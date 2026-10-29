package main

import (
	"bytes"
	"runtime/debug"
	"strings"
	"testing"

	raijincrawl "github.com/protonnegativo/raijin-crawl"
)

func TestBuildVersion(t *testing.T) {
	for _, c := range []struct {
		info debug.BuildInfo
		want string
	}{
		{debug.BuildInfo{Main: debug.Module{Version: "v0.1.3"}}, "v0.1.3"},
		{debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, "dev"},
		{debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef0123"}, {Key: "vcs.modified", Value: "false"}}}, "dev-0123456789ab"},
		{debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef0123"}, {Key: "vcs.modified", Value: "true"}}}, "dev-0123456789ab+dirty"},
	} {
		if got := buildVersion(&c.info); got != c.want {
			t.Errorf("buildVersion(%+v) = %q, want %q", c.info, got, c.want)
		}
	}
}

func TestVersionFlag(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"--version"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	e, _ := raijincrawl.Engines()
	want := "raijin-crawl " + version() + " (tested with katana " + e["katana"].Version + ")\n"
	if out.String() != want {
		t.Errorf("--version = %q, want %q", out.String(), want)
	}
	if !strings.HasPrefix(versionLine("v0.1.3"), "raijin-crawl v0.1.3 (tested with katana ") {
		t.Errorf("versionLine = %q", versionLine("v0.1.3"))
	}
}

func TestReleaseVersionWins(t *testing.T) {
	old := releaseVersion
	t.Cleanup(func() { releaseVersion = old })
	releaseVersion = "v9.9.9"
	if got := buildVersion(&debug.BuildInfo{Main: debug.Module{Version: "v0.1.3"}}); got != "v9.9.9" {
		t.Errorf("buildVersion = %q, want the linked release version", got)
	}
	if got := version(); got != "v9.9.9" {
		t.Errorf("version() = %q, want v9.9.9", got)
	}
}
