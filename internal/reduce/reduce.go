// Package reduce turns raw katana events into a small, ranked Raijin result.
//
// It is lossless for what an agent acts on: every URL katana reported is kept, with its method, status,
// content type and query parameter names, and every form with its method, action and fields. URLs that
// differ only in numeric, UUID or hash path segments, or in query values, share one route entry that
// still lists every URL. Only exact duplicates are dropped. A byte budget can cut, but every cut is
// counted in omitted.
package reduce

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/protonnegativo/raijin-crawl/internal/katana"
	"github.com/protonnegativo/raijin-crawl/internal/schema"
)

// Options control what the reducer keeps.
type Options struct {
	MaxItems int // 0 keeps every finding
	MaxBytes int
	InScope  func(target string) bool
}

// Defaults keep every finding and cap the JSON at a size that fits comfortably in an agent's context.
func Defaults() Options {
	return Options{MaxBytes: 16000}
}

// Stats are the counts of the crawl, before any budget, for the summary.
type Stats struct {
	URLs   int // distinct method and URL pairs katana requested or found
	Forms  int // distinct forms
	Routes int // findings and groups
}

// Output is what Reduce returns.
type Output struct {
	Findings []schema.Finding
	Groups   []schema.Group
	Omitted  map[string]int
	// Origin is the scheme and host every URL shares, or "" when there are several.
	Origin string
	// Locations are every location per finding fingerprint, before any budget, for --state.
	Locations map[string][]string
	Stats     Stats
}

// The rules that make a route a finding. Each tag has a fixed severity, and a finding takes the highest.
// file is high only when the file answered (2xx) or katana recorded no response; otherwise it is low.
var tagSeverity = map[string]string{
	"file":   "high",
	"admin":  "medium",
	"upload": "medium",
	"login":  "medium",
	"api":    "low",
	"params": "low",
	"form":   "low",
	"auth":   "low",
	"error":  "low",
}

var (
	numberSeg = regexp.MustCompile(`^[0-9]+$`)
	uuidSeg   = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	hashSeg   = regexp.MustCompile(`^[0-9a-fA-F]{16,}$`)

	// adminWords are path segments (without extension) of admin, debug, internal and config areas.
	adminWords = regexp.MustCompile(`^(admin\w*|administrator|wp-admin|_?debug\w*|internal|private|actuator|console|manage|management|manager|phpmyadmin|config|configuration|settings|backups?|setup|install|server-status|server-info|metrics|env|staff|dashboard|jmx|heapdump|trace|swagger|swagger-ui|api-docs|graphiql|_profiler|phpinfo|elmah)$`)
	// uploadWords are path segments of an upload or import endpoint.
	uploadWords = regexp.MustCompile(`^(upload\w*|file-?upload|import|attachments?)$`)
	// loginWords are path segments of a login, registration or password flow.
	loginWords = regexp.MustCompile(`^(log-?in|log-?on|sign-?in|sign-?up|register|auth|oauth\d?|sso|saml|forgot\w*|reset\w*|password)$`)
	// passwordParam is a field name that carries a password.
	passwordParam = regexp.MustCompile(`(?i)^(pass|passwd|password|pwd|pass_?word|new_?password|current_?password)$`)
	// apiWords are path segments of an API.
	apiWords = regexp.MustCompile(`^(api|apis|graphql|gql|rest|rpc|jsonrpc|v[0-9]+|soap|odata)$`)
)

// fileExt are extensions of backups, dumps, configs, archives, keys and logs.
var fileExt = set("bak", "old", "orig", "backup", "bkp", "swp", "save", "tmp", "sql", "db", "sqlite", "sqlite3",
	"dump", "mdb", "env", "ini", "conf", "cfg", "config", "yml", "yaml", "toml", "properties", "log", "zip", "tar",
	"gz", "tgz", "bz2", "xz", "7z", "rar", "war", "jar", "pem", "key", "p12", "pfx", "kdbx", "htpasswd", "git", "svn")

// staticExt are extensions of static assets; they become one group per extension.
var staticExt = set("css", "js", "mjs", "map", "png", "jpg", "jpeg", "gif", "svg", "ico", "webp", "bmp", "avif",
	"woff", "woff2", "ttf", "eot", "otf", "mp4", "webm", "mp3", "wav", "ogg", "pdf")

type entry struct {
	key     string
	route   string // the written route: [METHOD ][origin]pattern
	pattern string
	static  bool
	form    bool
	method  string
	status  int
	ctype   string
	enctype string
	err     string
	params  map[string]bool
	via     map[string]bool
	urls    map[string]bool
}

// item is one URL katana reported, or one form action.
type item struct {
	method, url, ctype, enctype, err, via string
	status                                int
	form                                  bool
	params                                []string
}

// Reduce collapses events into findings and groups: one entry per route and response, listing every URL
// of it.
func Reduce(events []katana.Event, opt Options) Output {
	out := Output{Omitted: map[string]int{}, Locations: map[string][]string{}}
	var items []item
	for _, ev := range events {
		if opt.InScope != nil && !opt.InScope(ev.Request.Endpoint) {
			out.Omitted["out_of_scope"]++
			continue
		}
		items = append(items, item{
			method: method(ev.Request.Method), url: ev.Request.Endpoint, status: ev.Status(),
			ctype: contentType(ev.ContentType(), ev.Status()), err: strings.TrimSpace(ev.Error),
			via: via(ev.Request), params: QueryParams(ev.Request.Endpoint),
		})
		for _, f := range ev.Forms() {
			action := f.Action
			if action == "" {
				action = ev.Request.Endpoint
			}
			if opt.InScope != nil && !opt.InScope(action) {
				out.Omitted["out_of_scope"]++
				continue
			}
			enctype := strings.ToLower(strings.TrimSpace(f.Enctype))
			if enctype == "application/x-www-form-urlencoded" {
				enctype = "" // the default, left out
			}
			params := append(QueryParams(action), f.Parameters...)
			items = append(items, item{method: method(f.Method), url: action, form: true, enctype: enctype, params: params})
		}
	}

	origins := map[string]bool{}
	for _, it := range items {
		origins[Origin(it.url)] = true
	}
	if len(origins) == 1 {
		for o := range origins {
			out.Origin = o
		}
	}

	entries := map[string]*entry{}
	var keys []string
	for _, it := range items {
		origin := Origin(it.url)
		pattern, static := Pattern(it.url)
		key := strings.Join([]string{it.method, origin, pattern, fmt.Sprint(static, it.form, it.status), it.ctype, it.enctype, it.err}, "\x00")
		e, ok := entries[key]
		if !ok {
			route := pattern
			if out.Origin == "" {
				route = origin + pattern
			}
			if it.method != "GET" {
				route = it.method + " " + route
			}
			e = &entry{key: key, route: route, pattern: pattern, static: static, form: it.form, method: it.method,
				status: it.status, ctype: it.ctype, enctype: it.enctype, err: it.err,
				params: map[string]bool{}, via: map[string]bool{}, urls: map[string]bool{}}
			entries[key] = e
			keys = append(keys, key)
		}
		if e.urls[it.url] {
			out.Omitted["duplicates"]++
			continue
		}
		e.urls[it.url] = true
		for _, p := range it.params {
			if p != "" {
				e.params[p] = true
			}
		}
		if it.via != "" {
			e.via[it.via] = true
		}
		if it.form {
			out.Stats.Forms++
		} else {
			out.Stats.URLs++
		}
	}

	for _, key := range keys {
		e := entries[key]
		locs := locations(e, out.Origin)
		id := ID(e.status, key)
		ep := schema.Endpoint{Route: e.route, Status: e.status, Type: e.ctype, Params: sorted(e.params), Enctype: e.enctype, Via: sorted(e.via), Error: e.err}
		tags := Tags(e)
		if len(tags) == 0 {
			out.Groups = append(out.Groups, schema.Group{ID: id, Endpoint: ep, Locations: locs, URLs: len(e.urls)})
			continue
		}
		f := schema.Finding{ID: id, Tags: tags, Endpoint: ep, Locations: locs, URLs: len(e.urls), Fingerprint: fingerprint(id)}
		for _, t := range tags {
			sev := tagSeverity[t]
			if t == "file" && !(e.status == 0 || e.status/100 == 2) {
				sev = "low"
			}
			if schema.SeverityRank[sev] > schema.SeverityRank[f.Severity] {
				f.Severity = sev
			}
		}
		out.Locations[f.Fingerprint] = locs
		if locs == nil {
			out.Locations[f.Fingerprint] = []string{e.route}
		}
		out.Findings = append(out.Findings, f)
	}
	out.Stats.Routes = len(keys)
	if out.Findings == nil {
		out.Findings = []schema.Finding{}
	}
	if out.Groups == nil {
		out.Groups = []schema.Group{}
	}

	sort.SliceStable(out.Findings, func(i, j int) bool {
		a, b := out.Findings[i], out.Findings[j]
		if schema.SeverityRank[a.Severity] != schema.SeverityRank[b.Severity] {
			return schema.SeverityRank[a.Severity] > schema.SeverityRank[b.Severity]
		}
		if schema.ConfidenceRank[a.Confidence] != schema.ConfidenceRank[b.Confidence] {
			return schema.ConfidenceRank[a.Confidence] > schema.ConfidenceRank[b.Confidence]
		}
		if len(a.Tags) != len(b.Tags) {
			return len(a.Tags) > len(b.Tags)
		}
		if a.URLs != b.URLs {
			return a.URLs > b.URLs
		}
		if a.Route != b.Route {
			return a.Route < b.Route
		}
		return a.ID < b.ID
	})
	sort.SliceStable(out.Groups, func(i, j int) bool {
		a, b := out.Groups[i], out.Groups[j]
		if a.URLs != b.URLs {
			return a.URLs > b.URLs
		}
		if a.Route != b.Route {
			return a.Route < b.Route
		}
		return a.ID < b.ID
	})

	if opt.MaxItems > 0 && len(out.Findings) > opt.MaxItems {
		out.Omitted["findings"] += len(out.Findings) - opt.MaxItems
		out.Findings = out.Findings[:opt.MaxItems]
	}
	return out
}

// Tags returns the rules a route matches, in a fixed order. No tags means it goes to a group. Static
// assets have no tags. A 404 or 410 has no path tags (file, admin, upload, login, api): the path is not
// there.
//
//	file    a backup, dump, config, archive, key or log extension (.bak .sql .env .zip ...), or a dotfile
//	admin   an admin, debug, internal, private, config or backup path segment
//	upload  a multipart form, or an upload or import path segment
//	login   a password field, or a login, register, auth or password-reset path segment
//	api     an api, graphql, rest or vN path segment, or a JSON or XML response
//	params  query parameters or form fields
//	form    an HTML form
//	auth    401, 403 or 407
//	error   5xx
func Tags(e *entry) []string {
	if e.static {
		return nil
	}
	path := strings.ToLower(e.pattern)
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	segs := strings.Split(strings.Trim(path, "/"), "/")
	var tags []string
	add := func(t string, ok bool) {
		if ok {
			tags = append(tags, t)
		}
	}
	gone := e.status == 404 || e.status == 410
	if !gone {
		last := segs[len(segs)-1]
		file := fileExt[ext(last)] || strings.HasSuffix(last, "~")
		admin, upload, login, api := false, e.enctype == "multipart/form-data", false, false
		for _, s := range segs {
			if len(s) > 1 && s[0] == '.' && s != ".." {
				file = true
			}
			word := strings.TrimSuffix(s, "."+ext(s))
			admin = admin || adminWords.MatchString(word)
			upload = upload || uploadWords.MatchString(word)
			login = login || loginWords.MatchString(word)
			api = api || apiWords.MatchString(word)
		}
		for p := range e.params {
			login = login || passwordParam.MatchString(p)
		}
		api = api || strings.Contains(e.ctype, "json") || strings.HasSuffix(e.ctype, "/xml")
		add("file", file)
		add("admin", admin)
		add("upload", upload)
		add("login", login)
		add("api", api)
	}
	add("params", len(e.params) > 0)
	add("form", e.form)
	add("auth", e.status == 401 || e.status == 403 || e.status == 407)
	add("error", e.status >= 500)
	return tags
}

// Pattern is the route of a URL: its path with numeric segments written {id}, UUIDs {uuid} and hex
// strings of 16 or more characters {hash}. A static asset (by extension) is "*.<ext>", and static is true.
func Pattern(raw string) (string, bool) {
	path := strings.TrimPrefix(raw, Origin(raw))
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	if path == "" {
		path = "/"
	}
	segs := strings.Split(path, "/")
	if e := strings.ToLower(ext(segs[len(segs)-1])); staticExt[e] {
		return "*." + e, true
	}
	for i, s := range segs {
		switch {
		case numberSeg.MatchString(s):
			segs[i] = "{id}"
		case uuidSeg.MatchString(s):
			segs[i] = "{uuid}"
		case hashSeg.MatchString(s):
			segs[i] = "{hash}"
		}
	}
	return strings.Join(segs, "/"), false
}

// Origin is the scheme and host of a URL as katana wrote it ("http://localhost:19500"), or "" when the
// URL has none.
func Origin(raw string) string {
	i := strings.Index(raw, "://")
	if i < 0 {
		return ""
	}
	rest := raw[i+3:]
	if j := strings.IndexAny(rest, "/?#"); j >= 0 {
		return raw[:i+3+j]
	}
	return raw
}

// QueryParams are the parameter names in a URL's query, decoded, in order of appearance.
func QueryParams(raw string) []string {
	i := strings.Index(raw, "?")
	if i < 0 {
		return nil
	}
	q := raw[i+1:]
	if j := strings.Index(q, "#"); j >= 0 {
		q = q[:j]
	}
	var out []string
	for _, kv := range strings.Split(q, "&") {
		name := strings.SplitN(kv, "=", 2)[0]
		if n, err := url.QueryUnescape(name); err == nil {
			name = n
		}
		if name != "" {
			out = append(out, name)
		}
	}
	return out
}

// locations are the URLs of an entry, without the shared origin, with numeric runs written as ranges.
// They are nil when the route has no variable segment and its only URL is the route itself.
func locations(e *entry, origin string) []string {
	var locs []string
	for u := range e.urls {
		if origin != "" {
			u = strings.TrimPrefix(u, origin)
		}
		locs = append(locs, escapeBrackets(u))
	}
	locs = Compress(locs)
	full := e.pattern
	if origin == "" {
		full = Origin(firstKey(e.urls)) + e.pattern
	}
	if !e.static && len(locs) == 1 && locs[0] == full && !strings.ContainsAny(e.pattern, "{}") {
		return nil
	}
	return locs
}

// escapeBrackets writes a URL's own [ and ] as %5B and %5D, so a bracket in a location is always a range.
func escapeBrackets(s string) string {
	return strings.NewReplacer("[", "%5B", "]", "%5D").Replace(s)
}

var digitRun = regexp.MustCompile(`[0-9]+`)

// Compress writes URLs that differ in one number as one location with the numbers as ranges:
// "/user/1" ... "/user/60" is "/user/[1-60]", and "/p?id=1", "/p?id=2", "/p?id=7" is "/p?id=[1-2,7]".
// It tries each position of a number in the URLs (the first number, the second...) and keeps the one that
// gives the fewest locations. A number with a leading zero is never part of a range. The result is sorted.
func Compress(urls []string) []string {
	uniq := map[string]bool{}
	for _, u := range urls {
		uniq[u] = true
	}
	best := sorted(uniq)
	most := 0
	for _, u := range best {
		most = max(most, len(digitRun.FindAllStringIndex(u, -1)))
	}
	for pos := 0; pos < most; pos++ {
		if out := compressAt(best, pos); len(out) < len(best) {
			best = out
		}
	}
	return best
}

func compressAt(urls []string, pos int) []string {
	type run struct {
		prefix, suffix string
		nums           []int64
	}
	runs := map[string]*run{}
	var order []string
	var out []string
	for _, u := range urls {
		idx := digitRun.FindAllStringIndex(u, -1)
		if pos >= len(idx) {
			out = append(out, u)
			continue
		}
		a, b := idx[pos][0], idx[pos][1]
		digits := u[a:b]
		var n int64
		if len(digits) > 18 || (len(digits) > 1 && digits[0] == '0') {
			out = append(out, u)
			continue
		}
		fmt.Sscan(digits, &n)
		k := u[:a] + "\x00" + u[b:]
		r, ok := runs[k]
		if !ok {
			r = &run{prefix: u[:a], suffix: u[b:]}
			runs[k] = r
			order = append(order, k)
		}
		r.nums = append(r.nums, n)
	}
	for _, k := range order {
		r := runs[k]
		if len(r.nums) == 1 {
			out = append(out, fmt.Sprint(r.prefix, r.nums[0], r.suffix))
			continue
		}
		out = append(out, r.prefix+"["+ranges(r.nums)+"]"+r.suffix)
	}
	sort.Strings(out)
	return out
}

// ranges writes sorted numbers as "1-5,7,9-12".
func ranges(nums []int64) string {
	sort.Slice(nums, func(i, j int) bool { return nums[i] < nums[j] })
	var parts []string
	for i := 0; i < len(nums); {
		j := i
		for j+1 < len(nums) && nums[j+1] == nums[j]+1 {
			j++
		}
		if j > i {
			parts = append(parts, fmt.Sprintf("%d-%d", nums[i], nums[j]))
		} else {
			parts = append(parts, fmt.Sprint(nums[i]))
		}
		i = j + 1
	}
	return strings.Join(parts, ",")
}

// ID is stable across runs while the route and its response do not change: the status (or "none" when
// katana recorded no response) and a short hash of the entry key.
func ID(status int, key string) string {
	head := fmt.Sprint(status)
	if status == 0 {
		head = "none"
	}
	return head + "-" + fingerprint(key)[:8]
}

// Fit shrinks the result until its JSON is at most maxBytes. It cuts the least important data first:
// group locations, then groups, then the locations of the lowest-ranked findings (each keeps one, and
// its count), then the lowest-ranked findings. Every cut is counted in Omitted.
func Fit(res *schema.Result, maxBytes int) {
	if maxBytes <= 0 || size(res) <= maxBytes {
		return
	}
	res.Budget.Truncated = true
	if res.Omitted == nil {
		res.Omitted = map[string]int{}
	}
	total := size(res) // now includes the budget object
	for i := len(res.Groups) - 1; i >= 0 && total > maxBytes; i-- {
		g := &res.Groups[i]
		if len(g.Locations) == 0 {
			continue
		}
		before := size(g)
		res.Omitted["locations"] += len(g.Locations)
		g.Locations, g.Count = nil, g.URLs
		total += size(g) - before
	}
	for total > maxBytes && len(res.Groups) > 0 {
		last := len(res.Groups) - 1
		total -= size(res.Groups[last]) + 1
		res.Groups = res.Groups[:last]
		res.Omitted["groups"]++
	}
	for i := len(res.Findings) - 1; i >= 0 && total > maxBytes; i-- {
		f := &res.Findings[i]
		if len(f.Locations) <= 1 {
			continue
		}
		before := size(f)
		res.Omitted["locations"] += len(f.Locations) - 1
		f.Locations, f.Count = f.Locations[:1], f.URLs
		total += size(f) - before
	}
	for total > maxBytes && len(res.Findings) > 1 {
		last := len(res.Findings) - 1
		total -= size(res.Findings[last]) + 1
		res.Findings = res.Findings[:last]
		res.Omitted["findings"]++
	}
	// the estimate above ignores the omitted counters it adds; settle on the real size
	for size(res) > maxBytes && len(res.Findings) > 1 {
		res.Findings = res.Findings[:len(res.Findings)-1]
		res.Omitted["findings"]++
	}
}

func method(m string) string {
	m = strings.ToUpper(strings.TrimSpace(m))
	if m == "" {
		return "GET"
	}
	return m
}

// contentType is the media type without parameters. text/html is left out (the documented default), and
// "none" means the server answered without a Content-Type.
func contentType(ct string, status int) string {
	ct = strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
	switch {
	case ct == "text/html":
		return ""
	case ct == "" && status != 0:
		return "none"
	}
	return ct
}

// via names where katana found an endpoint, when it was not a link in a page.
func via(r katana.Request) string {
	switch {
	case r.Attribute == "robotstxt":
		return "robots.txt"
	case r.Attribute == "sitemapxml":
		return "sitemap.xml"
	case r.Tag == "js":
		return "js"
	}
	return ""
}

// ext is the lowercase extension of a path segment, or "".
func ext(seg string) string {
	i := strings.LastIndex(seg, ".")
	if i <= 0 || i == len(seg)-1 {
		if i == 0 {
			return strings.ToLower(seg[1:]) // a dotfile: ".env" has extension "env"
		}
		return ""
	}
	return strings.ToLower(seg[i+1:])
}

func size(v any) int {
	b, _ := schema.Marshal(v)
	return len(b)
}

func set(items ...string) map[string]bool {
	m := map[string]bool{}
	for _, s := range items {
		m[s] = true
	}
	return m
}

func sorted(set map[string]bool) []string {
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func firstKey(set map[string]bool) string {
	for k := range set {
		return k
	}
	return ""
}

func fingerprint(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:8])
}
