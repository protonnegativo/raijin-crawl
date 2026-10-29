// Package schema defines the Raijin Schema, the JSON shape every Raijin tool returns.
//
// Nothing the engine found is dropped silently: every crawled URL is listed, in a finding or in a group,
// and anything a budget cuts is counted in Omitted. What an agent can rebuild is left out of the JSON to
// save tokens: empty lists, the budget when nothing was cut, a count equal to the URLs listed, and
// fields at their documented default.
//
// raijin-crawl adds the endpoint fields (route, status, type, params, enctype, via, error) to findings
// and groups, and Origin to the result: they are what an agent reads to pick its next requests.
package schema

import core "github.com/protonnegativo/raijin-core/schema"

// The envelope (Result, Error, Budget, Marshal, the ranks) lives in raijin-core and is shared by every
// Raijin tool; Finding and Group stay here because their fields are this tool's contract.
type (
	Result = core.Result[Finding, Group]
	Error  = core.Error
	Budget = core.Budget
)

var (
	// Marshal is json.Marshal without HTML escaping.
	Marshal = core.Marshal
	// Write writes a document followed by a newline, indented when pretty.
	Write = core.Write
	// SeverityRank orders severities from most to least important.
	SeverityRank = core.SeverityRank
	// ConfidenceRank orders confidence values: verified, then matched, then heuristic.
	ConfidenceRank = core.ConfidenceRank
)

// Endpoint is a route and what its URLs answered. Zero values are left out: no status means katana
// recorded no response (it does this when the body is identical to one it already saw, or when the
// request failed, see Error), no type means text/html ("none" when the server sent no Content-Type).
type Endpoint struct {
	// Route is the URL path with its variable segments replaced ({id}, {uuid}, {hash}), or "*.<ext>" for
	// a group of static assets. It starts with the method when the method is not GET, and with the
	// scheme and host when the crawl covered more than one origin.
	Route  string `json:"route"`
	Status int    `json:"status,omitempty"`
	Type   string `json:"type,omitempty"`
	// Params are the query parameter names of every URL of the route, and the field names of a form.
	Params []string `json:"params,omitempty"`
	// Enctype is a form's encoding when it is not application/x-www-form-urlencoded.
	Enctype string `json:"enctype,omitempty"`
	// Via says where katana found the route when it was not a link in a page: robots.txt, sitemap.xml
	// or js.
	Via   []string `json:"via,omitempty"`
	Error string   `json:"error,omitempty"`
}

// Finding is one route worth a look (parameters, a form, an API, an admin path, a backup file, a 403...)
// with every URL of it. The same tool and ID in two runs is the same finding.
type Finding struct {
	ID       string `json:"id"`
	Severity string `json:"severity"`
	// Confidence is "verified" or "heuristic"; empty means "matched".
	Confidence string `json:"confidence,omitempty"`
	// Tags are the rules that made it a finding: file, admin, upload, login, api, params, form, auth, error.
	Tags []string `json:"tags"`
	Endpoint
	// Locations are every URL of the route, numeric runs written as ranges ("/user/[1-60]"). They are
	// left out when the route has no variable segment and its only URL is the route itself.
	Locations []string `json:"locations,omitempty"`
	// Count is the number of URLs; it is set only when the budget cut Locations.
	Count int `json:"count,omitempty"`
	// URLs is the number of URLs, for ranking, the summary and Count; it is not written.
	URLs int `json:"-"`
	// Fingerprint is the internal key for --state; it is derived from the ID and not written.
	Fingerprint string `json:"-"`
	New         bool   `json:"new,omitempty"`
}

// Group is one route that no rule flagged (a plain page, a 404) or one kind of static asset (all the
// .css, all the .png), with every URL of it.
type Group struct {
	ID string `json:"id"`
	Endpoint
	Locations []string `json:"locations,omitempty"`
	// Count is the number of URLs; it is set only when the budget cut Locations.
	Count int `json:"count,omitempty"`
	URLs  int `json:"-"`
}
