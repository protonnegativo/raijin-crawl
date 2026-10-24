package reduce

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/protonnegativo/raijin-crawl/internal/katana"
	"github.com/protonnegativo/raijin-crawl/internal/schema"
	"github.com/protonnegativo/raijin-crawl/internal/scope"
)

func load(t *testing.T) []katana.Event {
	t.Helper()
	f, err := os.Open("../../testdata/sample.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	events, err := katana.Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

// ev is a GET katana reported with a status and an HTML response.
func ev(url string, status int) katana.Event {
	return katana.Event{
		Request:  katana.Request{Method: "GET", Endpoint: url, Tag: "a", Attribute: "href"},
		Response: &katana.Response{StatusCode: status, Headers: map[string]any{"content-type": "text/html"}},
	}
}

func byRoute(out Output) map[string][]schema.Endpoint {
	m := map[string][]schema.Endpoint{}
	for _, f := range out.Findings {
		m[f.Route] = append(m[f.Route], f.Endpoint)
	}
	for _, g := range out.Groups {
		m[g.Route] = append(m[g.Route], g.Endpoint)
	}
	return m
}

func TestReduce(t *testing.T) {
	events := load(t)
	if len(events) != 20 {
		t.Fatalf("parsed %d events, want 20 (the log line is skipped)", len(events))
	}
	sc, _ := scope.Parse([]string{"*.example.com"})
	opt := Defaults()
	opt.InScope = sc.Allowed
	out := Reduce(events, opt)

	if out.Origin != "https://app.example.com" {
		t.Errorf("origin = %q", out.Origin)
	}
	if out.Omitted["duplicates"] != 1 || out.Omitted["out_of_scope"] != 1 {
		t.Errorf("omitted = %v", out.Omitted)
	}
	if out.Stats.URLs != 18 || out.Stats.Forms != 3 || out.Stats.Routes != 17 {
		t.Errorf("stats = %+v", out.Stats)
	}
	var got []string
	for _, f := range out.Findings {
		got = append(got, f.Severity+" "+f.Route+" "+strings.Join(f.Tags, ","))
	}
	want := []string{
		"high /.env file",
		"medium POST /login login,params,form",
		"medium POST /upload upload,params,form",
		"medium /admin/ admin,auth",
		"medium /upload upload",
		"low /old/site.bak file,auth",
		"low /search params,form",
		"low /product params",
		"low /api/v1/items/{id} api",
		"low /error error",
		"low /list params",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("findings:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	login := out.Findings[1]
	if strings.Join(login.Params, ",") != "password,user" || login.Status != 0 || login.Locations != nil {
		t.Errorf("a form keeps its method, action and fields: %+v", login)
	}
	if up := out.Findings[2]; up.Enctype != "multipart/form-data" {
		t.Errorf("a multipart form keeps its encoding: %+v", up)
	}
	if env := out.Findings[0]; strings.Join(env.Via, ",") != "robots.txt" || env.Type != "text/plain" {
		t.Errorf("where katana found it and the type stay: %+v", env)
	}
	if g := out.Groups[0]; g.Route != "/user/{id}" || strings.Join(g.Locations, ",") != "/user/[1-3]" {
		t.Errorf("numeric segments collapse into one route that lists every URL: %+v", g)
	}
	routes := byRoute(out)
	if eps := routes["/user/{id}"]; len(eps) != 2 {
		t.Errorf("a URL with no recorded response is its own entry, not merged into the 200s: %+v", eps)
	}
	if eps := routes["/config.bak"]; len(eps) != 1 || eps[0].Status != 404 {
		t.Errorf("a 404 has no path tags and is a group: %+v", eps)
	}
	if eps := routes["/slow"]; len(eps) != 1 || eps[0].Error != "context deadline exceeded" {
		t.Errorf("katana's error stays: %+v", eps)
	}
}

// expand is the reader's side of a location: "[1-3,7]" is 1, 2, 3 and 7.
func expand(loc string) []string {
	m := regexp.MustCompile(`\[([0-9,\-]+)\]`).FindStringSubmatchIndex(loc)
	if m == nil {
		return []string{loc}
	}
	var out []string
	for _, part := range strings.Split(loc[m[2]:m[3]], ",") {
		a, b, _ := strings.Cut(part, "-")
		lo, _ := strconv.Atoi(a)
		hi := lo
		if b != "" {
			hi, _ = strconv.Atoi(b)
		}
		for n := lo; n <= hi; n++ {
			out = append(out, loc[:m[0]]+strconv.Itoa(n)+loc[m[1]:])
		}
	}
	return out
}

// Lossless rule: every URL katana reported is in exactly the locations of its entry, and its query
// parameter names are in the entry's params.
func TestEveryURLIsListed(t *testing.T) {
	var events []katana.Event
	var want []string
	add := func(u string) {
		events = append(events, ev(u, 200))
		want = append(want, u)
	}
	for i := 1; i <= 50; i++ {
		if i != 17 {
			add(fmt.Sprintf("http://h.test/user/%d", i))
		}
	}
	for i := 1; i <= 12; i++ {
		add(fmt.Sprintf("http://h.test/item?id=%d&page=%d", i, i%3))
	}
	add("http://h.test/user/007")
	add("http://h.test/blog/5d41402abc4b2a76b9719d911017c592")
	add("http://h.test/blog/7d793037a0760186574b0282f2f435e7")
	add("http://h.test/doc/2b1e7c3a-4f6e-4a6c-9b2e-1f0a2c3d4e5f")
	add("http://h.test/search?q=a+b&q=c")
	add("http://h.test/about")

	out := Reduce(events, Defaults())
	var got []string
	params := map[string]bool{}
	for _, g := range out.Groups {
		locs := g.Locations
		if locs == nil {
			locs = []string{g.Route}
		}
		for _, l := range locs {
			for _, u := range expand(l) {
				got = append(got, out.Origin+u)
			}
		}
		for _, p := range g.Params {
			params[g.Route+"?"+p] = true
		}
	}
	for _, f := range out.Findings {
		for _, l := range f.Locations {
			for _, u := range expand(l) {
				got = append(got, out.Origin+u)
			}
		}
		for _, p := range f.Params {
			params[f.Route+"?"+p] = true
		}
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("URLs listed:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	for _, p := range []string{"/item?id", "/item?page", "/search?q"} {
		if !params[p] {
			t.Errorf("param %s missing: %v", p, params)
		}
	}
	routes := byRoute(out)
	for _, r := range []string{"/user/{id}", "/blog/{hash}", "/doc/{uuid}", "/item", "/about"} {
		if len(routes[r]) != 1 {
			t.Errorf("route %s: %+v", r, routes[r])
		}
	}
}

func TestCompress(t *testing.T) {
	cases := []struct{ in, want []string }{
		{[]string{"/user/1", "/user/2", "/user/3", "/user/7", "/user/9", "/user/10"}, []string{"/user/[1-3,7,9-10]"}},
		{[]string{"/p?id=1&tab=a", "/p?id=2&tab=a", "/p?id=1", "/p?id=2"}, []string{"/p?id=[1-2]", "/p?id=[1-2]&tab=a"}},
		// the number that varies is the second one: /v1/ stays as it is
		{[]string{"/v1/x/5", "/v1/x/6", "/v1/x/8"}, []string{"/v1/x/[5-6,8]"}},
		// a leading zero is never part of a range
		{[]string{"/a/07", "/a/8", "/a/9"}, []string{"/a/07", "/a/[8-9]"}},
		{[]string{"/a/1"}, []string{"/a/1"}},
		{[]string{"/a/x", "/a/x"}, []string{"/a/x"}},
	}
	for _, c := range cases {
		if got := Compress(c.in); strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("Compress(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

// Lossless rule: a URL's own brackets are escaped, so a bracket in a location is always a range.
func TestBracketsInURLsAreEscaped(t *testing.T) {
	out := Reduce([]katana.Event{ev("http://h.test/a?f[]=1", 200), ev("http://h.test/a?f[]=2", 200)}, Defaults())
	f := out.Findings[0]
	if strings.Join(f.Locations, ",") != "/a?f%5B%5D=[1-2]" || strings.Join(f.Params, ",") != "f[]" {
		t.Errorf("got %+v", f)
	}
}

// Lossless rule: the same route with another status or type is another entry, so each URL keeps what it
// answered.
func TestOtherResponseIsAnotherEntry(t *testing.T) {
	a, b := ev("http://h.test/user/1", 200), ev("http://h.test/user/2", 403)
	c := ev("http://h.test/user/3", 200)
	c.Response.Headers = map[string]any{"Content-Type": "application/json; charset=utf-8"}
	out := Reduce([]katana.Event{a, b, c}, Defaults())
	if n := len(byRoute(out)["/user/{id}"]); n != 3 {
		t.Fatalf("got %d entries, want 3: %+v %+v", n, out.Findings, out.Groups)
	}
	for _, f := range out.Findings {
		if f.Status == 200 && f.Type != "application/json" {
			t.Errorf("type without parameters: %+v", f)
		}
	}
}

// Lossless rule: only exact repeats of a method and URL collapse, and they are counted.
func TestOnlyExactDuplicatesCollapse(t *testing.T) {
	post := ev("http://h.test/x", 200)
	post.Request.Method = "POST"
	out := Reduce([]katana.Event{ev("http://h.test/x", 200), ev("http://h.test/x", 200), post, ev("http://h.test/x?a=1", 200)}, Defaults())
	if out.Omitted["duplicates"] != 1 || out.Stats.URLs != 3 {
		t.Errorf("omitted = %v, stats = %+v", out.Omitted, out.Stats)
	}
	routes := byRoute(out)
	if len(routes["/x"]) != 1 || len(routes["POST /x"]) != 1 {
		t.Errorf("the method is part of the route: %v", routes)
	}
}

// Lossless rule: a form is kept with its method, action, fields and encoding, even when the same form is
// on many pages (counted as a duplicate) and even when its action is the page itself.
func TestFormsAreKept(t *testing.T) {
	page := ev("http://h.test/contact", 200)
	page.Response.Forms = []katana.Form{{Method: "post", Parameters: []string{"email", "msg"}}}
	other := ev("http://h.test/", 200)
	other.Response.Forms = []katana.Form{{Method: "POST", Action: "http://h.test/contact", Parameters: []string{"email", "msg"}}}
	out := Reduce([]katana.Event{page, other}, Defaults())
	eps := byRoute(out)["POST /contact"]
	if len(eps) != 1 || strings.Join(eps[0].Params, ",") != "email,msg" {
		t.Fatalf("form = %+v", eps)
	}
	if out.Stats.Forms != 1 || out.Omitted["duplicates"] != 1 {
		t.Errorf("stats = %+v, omitted = %v", out.Stats, out.Omitted)
	}
}

func TestOutOfScopeIsDropped(t *testing.T) {
	sc, _ := scope.Parse([]string{"h.test"})
	opt := Defaults()
	opt.InScope = sc.Allowed
	page := ev("http://h.test/", 200)
	page.Response.Forms = []katana.Form{{Method: "POST", Action: "https://other.test/collect", Parameters: []string{"x"}}}
	out := Reduce([]katana.Event{page, ev("http://other.test/", 200)}, opt)
	if out.Omitted["out_of_scope"] != 2 || out.Stats.URLs != 1 || out.Stats.Forms != 0 {
		t.Errorf("omitted = %v, stats = %+v", out.Omitted, out.Stats)
	}
}

func TestSeverityRules(t *testing.T) {
	cases := []struct {
		url    string
		status int
		sev    string
		tags   string
	}{
		{"http://h.test/backup.sql", 200, "high", "file,admin"},
		{"http://h.test/dump.sql", 200, "high", "file"},
		{"http://h.test/.git/config", 200, "high", "file,admin"},
		{"http://h.test/site.zip", 403, "low", "file,auth"},
		{"http://h.test/admin/users", 200, "medium", "admin"},
		{"http://h.test/admin/users", 404, "", ""},
		{"http://h.test/graphql", 400, "low", "api"},
		{"http://h.test/api/v2/users?id=1", 200, "low", "api,params"},
		{"http://h.test/signin", 200, "medium", "login"},
		{"http://h.test/report", 502, "low", "error"},
	}
	for _, c := range cases {
		out := Reduce([]katana.Event{ev(c.url, c.status)}, Defaults())
		sev, tags := "", ""
		if len(out.Findings) == 1 {
			sev, tags = out.Findings[0].Severity, strings.Join(out.Findings[0].Tags, ",")
		}
		if sev != c.sev || tags != c.tags {
			t.Errorf("%s %d: %s %s, want %s %s", c.url, c.status, sev, tags, c.sev, c.tags)
		}
	}
}

func TestStaticAssetsAreGroupsByExtension(t *testing.T) {
	var events []katana.Event
	for i := 1; i <= 5; i++ {
		e := ev(fmt.Sprintf("http://h.test/js/chunk-%d.js?v=3", i), 200)
		e.Response.Headers = map[string]any{"content-type": "application/javascript"}
		events = append(events, e)
	}
	events = append(events, ev("http://h.test/admin/app.css", 200))
	out := Reduce(events, Defaults())
	if len(out.Findings) != 0 || len(out.Groups) != 2 {
		t.Fatalf("static assets are never findings: %+v %+v", out.Findings, out.Groups)
	}
	if g := out.Groups[0]; g.Route != "*.js" || strings.Join(g.Locations, ",") != "/js/chunk-[1-5].js?v=3" || strings.Join(g.Params, ",") != "v" {
		t.Errorf("js group = %+v", g)
	}
}

func TestSeveralOriginsKeepFullURLs(t *testing.T) {
	out := Reduce([]katana.Event{ev("http://a.test/x", 200), ev("https://b.test:8443/user/1", 200), ev("https://b.test:8443/user/2", 200)}, Defaults())
	if out.Origin != "" {
		t.Errorf("origin = %q", out.Origin)
	}
	routes := byRoute(out)
	if len(routes["http://a.test/x"]) != 1 || len(routes["https://b.test:8443/user/{id}"]) != 1 {
		t.Errorf("routes = %v", routes)
	}
	for _, g := range out.Groups {
		if g.Route == "https://b.test:8443/user/{id}" && strings.Join(g.Locations, ",") != "https://b.test:8443/user/[1-2]" {
			t.Errorf("locations = %v", g.Locations)
		}
		if g.Route == "http://a.test/x" && g.Locations != nil {
			t.Errorf("a location equal to the route is left out: %v", g.Locations)
		}
	}
}

func TestOrderIsStable(t *testing.T) {
	events := load(t)
	a := Reduce(events, Defaults())
	for i := 0; i < 5; i++ {
		b := Reduce(events, Defaults())
		x, _ := schema.Marshal(a.Findings)
		y, _ := schema.Marshal(b.Findings)
		g1, _ := schema.Marshal(a.Groups)
		g2, _ := schema.Marshal(b.Groups)
		if string(x) != string(y) || string(g1) != string(g2) {
			t.Fatal("the same input must give the same output")
		}
	}
}

func TestMaxItemsCountsCuts(t *testing.T) {
	opt := Defaults()
	opt.MaxItems = 2
	out := Reduce(load(t), opt)
	if len(out.Findings) != 2 || out.Omitted["findings"] != 9 {
		t.Errorf("findings = %d, omitted = %v", len(out.Findings), out.Omitted)
	}
}

func TestFitCutsLeastImportantFirst(t *testing.T) {
	var events []katana.Event
	for i := 0; i < 300; i++ {
		events = append(events, ev(fmt.Sprintf("http://h.test/a/f%016x", i), 200))
	}
	events = append(events, ev("http://h.test/admin/", 403))
	out := Reduce(events, Defaults())
	listed := len(out.Groups[0].Locations)
	res := schema.Result{Tool: "raijin-crawl", Findings: out.Findings, Groups: out.Groups, Omitted: map[string]int{}, Budget: schema.Budget{MaxBytes: 2000}}
	Fit(&res, 2000)
	b, _ := schema.Marshal(res)
	if len(b) > 2000 {
		t.Errorf("output is %d bytes, over the budget", len(b))
	}
	if !res.Budget.Truncated || res.Omitted["locations"] != listed || len(res.Findings) != 1 {
		t.Errorf("budget = %+v, omitted = %v, findings = %d", res.Budget, res.Omitted, len(res.Findings))
	}
	if g := res.Groups[0]; g.Locations != nil || g.Count != 300 || !strings.Contains(string(b), `"count":300`) {
		t.Errorf("a cut group keeps its count: %+v", g)
	}
}
