// Package katana runs the ProjectDiscovery katana crawler and parses its JSONL output.
package katana

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/protonnegativo/raijin-crawl/internal/scope"
)

// ErrNotFound is returned when the katana binary is not on PATH.
var ErrNotFound = errors.New("katana binary not found")

// Event is the subset of a katana JSONL result that Raijin uses: one request katana made (or found),
// and what came back.
type Event struct {
	Request  Request   `json:"request"`
	Response *Response `json:"response"`
	Error    string    `json:"error"`
}

// Request is the endpoint katana visited and where it found it.
type Request struct {
	Method   string `json:"method"`
	Endpoint string `json:"endpoint"`
	// Tag and Attribute say where the endpoint came from: "a"/"href", "script"/"src", "file"/"robotstxt",
	// "js"/"jsluice-fetch" and so on. The crawl's start URL has neither.
	Tag       string `json:"tag"`
	Attribute string `json:"attribute"`
	Source    string `json:"source"`
}

// Response is what the endpoint answered. katana writes an empty response (no status) when the body is
// identical to one it has already seen in the crawl.
type Response struct {
	StatusCode    int            `json:"status_code"`
	Headers       map[string]any `json:"headers"`
	ContentLength int            `json:"content_length"`
	Forms         []Form         `json:"forms"`
}

// Form is an HTML form katana extracted from a page (-fx).
type Form struct {
	Method     string   `json:"method"`
	Action     string   `json:"action"`
	Enctype    string   `json:"enctype"`
	Parameters []string `json:"parameters"`
}

// Status is the response status, or 0 when katana recorded no response.
func (e Event) Status() int {
	if e.Response == nil {
		return 0
	}
	return e.Response.StatusCode
}

// ContentType is the Content-Type header as katana wrote it, or "".
func (e Event) ContentType() string {
	if e.Response == nil {
		return ""
	}
	for k, v := range e.Response.Headers {
		if strings.EqualFold(k, "content-type") {
			if l, ok := v.([]any); ok && len(l) > 0 {
				v = l[0]
			}
			return fmt.Sprint(v)
		}
	}
	return ""
}

// Forms are the forms katana extracted from the response.
func (e Event) Forms() []Form {
	if e.Response == nil {
		return nil
	}
	return e.Response.Forms
}

// Options are the katana settings Raijin passes through.
type Options struct {
	Binary string
	// Depth is the maximum crawl depth (katana -d). Known files need at least 3.
	Depth int
	// JS parses JavaScript files for endpoints (katana -jc -jsl).
	JS bool
	// KnownFiles is katana -kf: all, robotstxt or sitemapxml. Empty disables it.
	KnownFiles  string
	Duration    string
	Rate        int
	Timeout     int
	Concurrency int
	// Hosts are the target hosts; the crawl never follows a link to another host (katana -cs).
	Hosts []string
	Extra []string
	// RawPath, if set, receives katana's JSONL output unchanged.
	RawPath string
}

// Run crawls the targets and returns every event and the parse stats, plus the last stderr lines if katana failed.
func Run(ctx context.Context, targets []string, opt Options) ([]Event, Stats, string, error) {
	bin := opt.Binary
	if bin == "" {
		bin = "katana"
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return nil, Stats{}, "", ErrNotFound
	}

	list, err := os.CreateTemp("", "raijin-crawl-targets-*.txt")
	if err != nil {
		return nil, Stats{}, "", err
	}
	defer os.Remove(list.Name())
	if _, err := io.WriteString(list, strings.Join(targets, "\n")+"\n"); err != nil {
		list.Close()
		return nil, Stats{}, "", err
	}
	list.Close()

	// katana reads targets from stdin when stdin is not a terminal; Stdin stays nil (/dev/null) so a
	// caller's pipe is never read as a target list.
	cmd := exec.CommandContext(ctx, path, Args(list.Name(), opt)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, Stats{}, "", err
	}
	var r io.Reader = stdout
	if opt.RawPath != "" {
		raw, err := os.Create(opt.RawPath)
		if err != nil {
			return nil, Stats{}, "", err
		}
		defer raw.Close()
		r = io.TeeReader(stdout, raw)
	}
	if err := cmd.Start(); err != nil {
		return nil, Stats{}, "", err
	}
	events, st, parseErr := ParseStats(r)
	waitErr := cmd.Wait()
	if parseErr != nil {
		return events, st, tail(stderr.String(), 3), parseErr
	}
	if waitErr != nil {
		return events, st, tail(stderr.String(), 3), fmt.Errorf("katana exited: %w", waitErr)
	}
	return events, st, "", nil
}

// Args builds the katana command line. -or drops the raw request and response strings, which nothing
// reads; the body is kept because katana parses JavaScript from it (-ob also stops -jsl from finding
// endpoints). -fx writes the forms of each page.
func Args(listFile string, opt Options) []string {
	args := []string{"-list", listFile, "-jsonl", "-silent", "-nc", "-duc", "-or", "-fx"}
	if opt.Depth > 0 {
		args = append(args, "-d", fmt.Sprint(opt.Depth))
	}
	if opt.JS {
		args = append(args, "-jc", "-jsl")
	}
	if opt.KnownFiles != "" {
		args = append(args, "-kf", opt.KnownFiles)
	}
	if opt.Duration != "" {
		args = append(args, "-ct", opt.Duration)
	}
	if opt.Rate > 0 {
		args = append(args, "-rl", fmt.Sprint(opt.Rate))
	}
	if opt.Timeout > 0 {
		args = append(args, "-timeout", fmt.Sprint(opt.Timeout))
	}
	if opt.Concurrency > 0 {
		args = append(args, "-c", fmt.Sprint(opt.Concurrency))
	}
	if re := HostScope(opt.Hosts); re != "" {
		args = append(args, "-cs", re)
	}
	return append(args, opt.Extra...)
}

// HostScope is the katana crawl scope (-cs) that keeps the crawl on the given hosts, any port.
func HostScope(hosts []string) string {
	seen := map[string]bool{}
	var alts []string
	for _, h := range hosts {
		h = scope.Hostname(h)
		if h == "" || seen[h] {
			continue
		}
		seen[h] = true
		alts = append(alts, regexp.QuoteMeta(h))
	}
	if len(alts) == 0 {
		return ""
	}
	return `^https?://(` + strings.Join(alts, "|") + `)(:[0-9]+)?([/?#]|$)`
}

// Stats counts the output lines Parse read and the ones that did not look like katana results.
type Stats struct {
	// Lines are the non-empty lines that are not katana log lines.
	Lines int
	// Unparsed are the lines that are not JSON objects with a request endpoint; they are not events.
	Unparsed int
	// NoResult are the events with neither a response nor an error; katana writes one of them on
	// every result, an empty response when the body was already seen.
	NoResult int
}

// Parse reads katana JSONL. Lines that are not JSON objects with an endpoint are skipped.
func Parse(r io.Reader) ([]Event, error) {
	events, _, err := ParseStats(r)
	return events, err
}

// ParseStats is Parse, and also counts the lines that did not have the fields Raijin reads.
func ParseStats(r io.Reader) ([]Event, Stats, error) {
	var events []Event
	var st Stats
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		// katana's log lines ("[INF] ...") are not results, when an --input file has them.
		if len(line) == 0 || line[0] == '[' {
			continue
		}
		st.Lines++
		var ev struct {
			Event
			Raw map[string]json.RawMessage
		}
		if line[0] != '{' || json.Unmarshal(line, &ev.Event) != nil || json.Unmarshal(line, &ev.Raw) != nil {
			st.Unparsed++
			continue
		}
		if ev.Request.Endpoint == "" {
			st.Unparsed++
			continue
		}
		if _, ok := ev.Raw["response"]; !ok && ev.Error == "" {
			st.NoResult++
		}
		events = append(events, ev.Event)
	}
	return events, st, sc.Err()
}

// FormatHint is the engine_format error hint when katana wrote lines without the fields Raijin reads
// (request.endpoint, and response or error), or "" when every line had them.
func FormatHint(st Stats, raw string) string {
	bad := st.Unparsed + st.NoResult
	if bad == 0 {
		return ""
	}
	hint := fmt.Sprintf("%d of %d katana results had no request.endpoint or no response/error; the engine's output format may have changed", bad, st.Lines)
	if raw != "" {
		hint += ". Read " + raw + " for the full output"
	}
	return hint
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}
