package katana

import (
	"regexp"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	in := `[INF] Started standard crawling
{"request":{"method":"GET","endpoint":"http://h.test/login"},"response":{"status_code":200,"headers":{"Content-Type":"text/html; charset=utf-8"},"forms":[{"method":"POST","action":"http://h.test/login","parameters":["user","password"]}]}}
{"request":{"method":"GET","endpoint":"http://h.test/user/2","tag":"a","attribute":"href"},"response":{"headers":{}}}
{"request":{"method":"GET","endpoint":"http://h.test/slow"},"error":"context deadline exceeded"}
{"request":{}}
not json
`
	events, err := Parse(strings.NewReader(in))
	if err != nil || len(events) != 3 {
		t.Fatalf("got %d events, err %v", len(events), err)
	}
	if e := events[0]; e.Status() != 200 || e.ContentType() != "text/html; charset=utf-8" || len(e.Forms()) != 1 {
		t.Errorf("first event = %+v", e)
	}
	if e := events[1]; e.Status() != 0 || e.ContentType() != "" {
		t.Errorf("an empty response has no status: %+v", e)
	}
	if e := events[2]; e.Status() != 0 || e.Error != "context deadline exceeded" || e.Forms() != nil {
		t.Errorf("a failed request keeps its error: %+v", e)
	}
}

func TestArgs(t *testing.T) {
	args := strings.Join(Args("list.txt", Options{Depth: 3, JS: true, KnownFiles: "all", Rate: 50, Hosts: []string{"http://localhost:19500/", "a.example.com"}, Extra: []string{"-H", "X: y"}}), " ")
	for _, want := range []string{"-list list.txt -jsonl -silent -nc -duc -or -fx", "-d 3", "-jc -jsl", "-kf all", "-rl 50", "-H X: y"} {
		if !strings.Contains(args, want) {
			t.Errorf("args %q should contain %q", args, want)
		}
	}
	if strings.Contains(args, "-ob") {
		t.Errorf("-ob stops katana from parsing JavaScript: %q", args)
	}
	if !strings.Contains(args, `-cs ^https?://(localhost|a\.example\.com)(:[0-9]+)?([/?#]|$)`) {
		t.Errorf("crawl scope: %q", args)
	}
}

func TestHostScopeStaysOnHost(t *testing.T) {
	re := regexp.MustCompile(HostScope([]string{"http://localhost:19500/"}))
	for u, want := range map[string]bool{
		"http://localhost:19500/x":      true,
		"https://localhost/":            true,
		"http://localhost":              true,
		"http://localhost.evil.test/":   false,
		"http://evil.test/?localhost":   false,
		"http://localhostx:19500/":      false,
		"http://user@localhost:19500/x": false,
		"http://127.0.0.1:19500/x":      false,
		"http://localhost:19500?a=1":    true,
	} {
		if re.MatchString(u) != want {
			t.Errorf("%s: match %v, want %v", u, !want, want)
		}
	}
}

func TestParseVersion(t *testing.T) {
	for in, want := range map[string]string{
		"\n\x1b[34mINF\x1b[0m] Current version: v1.1.2\n": "1.1.2",
		"[INF] Current version: v1.0.5\n":                 "1.0.5",
		"flag provided but not defined: -version":         "",
		"": "",
	} {
		if got := ParseVersion(in); got != want {
			t.Errorf("ParseVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUntested(t *testing.T) {
	for _, c := range []struct {
		installed, tested string
		want              bool
	}{
		{"1.1.2", "1.1.2", false},
		{"1.1.3", "1.1.2", false},
		{"1.4.0", "1.1.2", false},
		{"1.1.1", "1.1.2", true},
		{"1.0.9", "1.1.2", true},
		{"2.0.0", "1.1.2", true},
		{"0.9.0", "1.1.2", true},
		{"", "1.1.2", false},
	} {
		if got := Untested(c.installed, c.tested); got != c.want {
			t.Errorf("Untested(%q, %q) = %v, want %v", c.installed, c.tested, got, c.want)
		}
	}
	if VersionHint("1.1.2", "1.1.2", "x") != "" || !strings.Contains(VersionHint("1.0.0", "1.1.2", "go install m@v1.1.2"), "katana 1.0.0 is not the tested 1.1.2") {
		t.Error("VersionHint")
	}
}

func TestFormatRenamedField(t *testing.T) {
	// katana renamed request.endpoint to request.url: no line is an event, and all are counted.
	in := `{"request":{"method":"GET","url":"http://h.test/"},"response":{"status_code":200}}
{"request":{"method":"GET","url":"http://h.test/a"},"response":{"status_code":200}}
`
	events, st, err := ParseStats(strings.NewReader(in))
	if err != nil || len(events) != 0 || st.Lines != 2 || st.Unparsed != 2 {
		t.Fatalf("events %d, stats %+v, err %v", len(events), st, err)
	}
	if h := FormatHint(st, "/tmp/raw.jsonl"); !strings.HasPrefix(h, "2 of 2 katana results had no") || !strings.HasSuffix(h, "Read /tmp/raw.jsonl for the full output") {
		t.Errorf("hint %q", h)
	}
	// response renamed: the events stay, but are counted.
	in = `{"request":{"method":"GET","endpoint":"http://h.test/"},"resp":{"status_code":200}}
{"request":{"method":"GET","endpoint":"http://h.test/a"},"response":{"headers":{}}}
`
	events, st, _ = ParseStats(strings.NewReader(in))
	if len(events) != 2 || st.NoResult != 1 || FormatHint(st, "") == "" {
		t.Errorf("events %d, stats %+v", len(events), st)
	}
	if FormatHint(Stats{Lines: 3}, "") != "" {
		t.Error("clean output has no hint")
	}
}
