# ⚡ Raijin Crawl

[![CI](https://github.com/protonnegativo/raijin-crawl/actions/workflows/ci.yml/badge.svg)](https://github.com/protonnegativo/raijin-crawl/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/protonnegativo/raijin-crawl)](https://github.com/protonnegativo/raijin-crawl/releases/latest)
[![License: MIT](https://img.shields.io/github/license/protonnegativo/raijin-crawl)](LICENSE)
[![Go Reference](https://pkg.go.dev/badge/github.com/protonnegativo/raijin-crawl.svg)](https://pkg.go.dev/github.com/protonnegativo/raijin-crawl)
[![Go Report Card](https://goreportcard.com/badge/github.com/protonnegativo/raijin-crawl)](https://goreportcard.com/report/github.com/protonnegativo/raijin-crawl)

**katana for AI agents: every endpoint and parameter, URLs collapsed by route pattern (`/user/{id}`), forms, APIs, admin paths and backup files ranked first.**

`raijin-crawl` is a tool of **Raijin**, an arsenal of offensive security tools built for AI agents. It runs [katana](https://github.com/projectdiscovery/katana) underneath and returns the **Raijin Schema**: one compact JSON where URLs that differ only in an ID, a UUID, a hash or a query value share one route entry, forms, parameters, API routes, admin and debug paths, backup and config files and 401/403/5xx answers are ranked findings, static assets are one group per extension, and anything it had to cut is counted.

## Why not just run katana?

katana is built for people and pipelines. An agent pays for every byte it reads. Its JSONL output is one line per URL with the raw request, the raw response, the headers and the body, so a crawl of a catalog with 60 user pages is 60 copies of the same template. `-or -ob` drops the raw strings and bodies, but every line still repeats the timestamp, the headers, the source page and the field names. The text output is one URL per line: no status, no method, no forms and no field names.

Here is the same crawl, with the same URLs, read by an agent (tokens, `o200k_base`):

```
Noisy crawl: 215 URLs, 60 user pages, 80 product URLs, 31 JS files
  raijin-crawl           ▎                         1,372 tokens
  katana text            ▌                         3,237 tokens
  katana -jsonl, no body ████                     23,158 tokens
  katana -jsonl          ████████████████████    115,723 tokens

Findings crawl: 32 URLs and 4 forms, 21 findings (3 high, 9 medium, 9 low)
  raijin-crawl           █▉                        1,176 tokens
  katana text            ▋                           396 tokens
  katana -jsonl, no body █████▉                    3,560 tokens
  katana -jsonl          ████████████████████     12,098 tokens
```

On the findings crawl, katana's text output is smaller: 396 tokens against 1,176 for raijin-crawl. The text is only a list of URLs, while raijin-crawl also gives the status of each URL, the 4 forms (`POST /login` with a `password` field, a multipart `POST /upload`), where each endpoint came from (`robots.txt`, `sitemap.xml`, a JS file), and the `.env`, SQL dump and 401/403 findings ranked first. If the crawl is small, every URL is different and you only need the list, the text output is the cheapest choice. raijin-crawl pays off on a large, repetitive crawl: on the noisy crawl the text costs 3,237 tokens and raijin-crawl 1,372, because `/user/1..60` becomes one entry.

raijin-crawl reads **84×** fewer tokens than `katana -jsonl` on the noisy crawl and **10.3×** fewer on the findings crawl. Against katana's JSONL without the raw strings and bodies (what `-or -ob` writes; see [BENCHMARK.md](BENCHMARK.md)) it is **17×** and **3.0×** smaller. Each crawl's bars are drawn to scale against `katana -jsonl`.

- **Nothing is lost.** Every URL katana reported is in the output with its method, status, content type and query parameter names, and every form with its method, action, fields and encoding. `bench/verify.py` checked all 247 katana results and 4 forms in both crawls and none were missing.
- **Routes, not URLs.** `/user/1` to `/user/60` is one route, `/user/{id}`, with one location, `/user/[1-60]`. 40 products with a reviews tab are `/product?id=[1-40]&tab=reviews`. A hash or a UUID is a `{hash}` or `{uuid}` segment, and each one is still listed.
- **The real targets come first.** A `.env` and a SQL dump found in `robots.txt`, a login and an upload form, an admin API found in a JS file and a 403 are findings, ranked by severity. Static assets and plain pages are groups.
- **Output is bounded.** On a huge crawl, `--max-bytes` cuts the least important data first and counts every cut. katana's output grows with every URL.
- **Details are one file away.** katana's full JSONL is saved to disk, and its path is in `raw`. It holds the headers, the bodies, the source page of each link and the timestamps, costs no tokens until the agent opens it, and replays with `--input`.

Against katana's text output, raijin-crawl is **2.4×** smaller on the noisy crawl and **3.0× larger** on the findings crawl. When every route is different there is little to collapse, and the status, type, parameters, IDs, tags and severities cost more than a bare URL list. The text output also has no status codes, no methods, and none of the 4 forms: an agent reading it does not know there is a `POST /login` with a `password` field or a multipart upload. Full method and numbers are in [BENCHMARK.md](BENCHMARK.md), and `bench/run.sh` reproduces them.

## Install

```bash
go install github.com/protonnegativo/raijin-crawl/cmd/raijin-crawl@latest
```

`@latest` installs the latest release. You also need ProjectDiscovery `katana` on `PATH`, or pass `--katana <path>`.

### Versions

Releases are tagged automatically on every change and on every passing engine update. `raijin-crawl --version` shows the release and the tested engine versions, e.g. `raijin-crawl v0.1.0 (tested with katana 1.8.0)`. `raijin-mcp update` updates every Raijin tool at once.

## Usage

```bash
raijin-crawl --scope scope.txt                                  # crawl every host the scope names
raijin-crawl --scope scope.txt -u https://app.example.com/
raijin-crawl --scope scope.txt -l start-urls.txt --depth 4 --duration 5m
raijin-crawl --scope scope.txt --input katana-results.jsonl     # reduce output you already have
raijin-crawl --scope scope.txt -u https://app.example.com/ --state run.json   # mark findings that are new since last run
raijin-crawl ... -- -H 'Cookie: session=...' -hl                # anything after -- goes to katana
```

`--scope` is required. One rule per line:

```
example.com
*.example.com
10.0.0.0/24
!admin.example.com
```

With no `-u`, `-l` or `--input`, raijin-crawl crawls what the scope names: every domain, every IP, and every address of ranges up to a /24, minus exclusions. Each bare host goes to katana as is, and katana starts it at `https://<host>/`; `next` points to `raijin-probe` for the live web ports. A wildcard names no host and a larger range is too big to sweep blindly, so for each one `next` gives the command that enumerates it first (`raijin-subs` or `raijin-ports`).

Start URLs outside the scope are dropped before katana runs. The crawl stays on the hosts of the start URLs (katana `-cs`, any port), and results and form actions outside the scope are dropped from the output. Both are counted in `omitted.out_of_scope`.

What raijin-crawl asks katana for by default: depth 3 (`-d 3`), JavaScript parsing (`-jc -jsl`: jsluice finds the `fetch()` and string endpoints in JS files), known files (`-kf all`: `robots.txt` and `sitemap.xml`) and forms (`-fx`). `--js=false`, `--known-files none` and `--depth` change them. raijin-crawl passes `-or` (no raw request and response strings) but not `-ob`: katana 1.1.2 finds no JS endpoints when it omits the bodies.

## Output

```json
{
  "schema_version": 1,
  "tool": "raijin-crawl",
  "version": "v0.1.0",
  "target": "http://localhost:19501/",
  "summary": "32 URLs, 4 forms, 31 routes: 21 findings (3 high, 9 medium, 9 low), 10 groups",
  "origin": "http://localhost:19501",
  "findings": [
    {
      "id": "200-a2adf96a",
      "severity": "high",
      "tags": ["file", "admin"],
      "route": "/backup/db.sql",
      "status": 200,
      "type": "application/sql",
      "via": ["robots.txt"]
    },
    {
      "id": "200-e5a7db04",
      "severity": "high",
      "tags": ["file"],
      "route": "/.env",
      "status": 200,
      "type": "text/plain",
      "via": ["robots.txt"]
    },
    {
      "id": "403-50995fcb",
      "severity": "medium",
      "tags": ["admin", "api", "auth"],
      "route": "/api/v2/admin/settings",
      "status": 403,
      "type": "application/json",
      "via": ["js"]
    },
    {
      "id": "none-3fa9e68f",
      "severity": "medium",
      "tags": ["login", "params", "form"],
      "route": "POST /login",
      "params": ["csrf_token", "password", "username"]
    },
    {
      "id": "none-4f613bba",
      "severity": "medium",
      "tags": ["upload", "params", "form"],
      "route": "POST /upload",
      "params": ["document", "title"],
      "enctype": "multipart/form-data"
    },
    {
      "id": "none-5b02a6c8",
      "severity": "low",
      "tags": ["api", "params"],
      "route": "/api/v1/orders",
      "params": ["page", "status"],
      "via": ["js"],
      "locations": ["/api/v1/orders?status=open&page=1"]
    },
    {
      "id": "none-8339ea85",
      "severity": "low",
      "tags": ["api"],
      "route": "/api/v1/users/{id}",
      "locations": ["/api/v1/users/[1-2]"]
    }
  ],
  "groups": [
    {
      "id": "none-0c3a7d98",
      "route": "/user/{id}",
      "locations": ["/user/[1-3]"]
    },
    {
      "id": "200-a978d8ab",
      "route": "*.js",
      "status": 200,
      "type": "application/javascript",
      "locations": ["/static/js/app.js", "/static/js/site.js"]
    }
  ],
  "raw": "bench/fixtures/findings/raw.jsonl"
}
```

That is the real findings crawl from the benchmark, pretty-printed here. Three things are shortened: 14 of the 21 findings and 8 of the 10 groups are left out, short arrays are written on one line, and `raw` is relative to the repo. The tool writes it as one line. The noisy crawl's biggest entries are one line each:

```json
{"id":"200-07922aa9","route":"/user/{id}","status":200,"locations":["/user/[1-60]"]}
{"id":"none-4ff3bacf","severity":"low","tags":["params"],"route":"/product","params":["id","tab"],"locations":["/product?id=[1-40]&tab=reviews"]}
```

**Nothing katana found is dropped silently.** Every URL is in exactly one finding or group. Only exact repeats collapse, and they are counted in `omitted.duplicates`.

What the reducer does:

- **One entry per route and response.** A route is the method and the URL path with variable segments replaced: a number is `{id}`, a UUID is `{uuid}`, and a hex string of 16 or more characters (an MD5, SHA-1 or SHA-256) is `{hash}`. Query values never split a route. URLs of the same route that answered with another status, content type or error are another entry with the same route, so each URL keeps what it answered.
- **`params`** is the union of the query parameter names of every URL of the entry, plus the field names of a form. Each URL still carries its own query in `locations`.
- **`locations`** lists every URL of the entry, without the `origin`, sorted. URLs that differ in one number are one location with the numbers as a range: `/user/[1-60]`, `/p?id=[1-3,7]`. raijin-crawl tries each number in the URL (the first, the second...) and keeps the one that gives the fewest locations. A number with a leading zero (`007`) is never part of a range. A URL's own `[` and `]` are written `%5B` and `%5D`, so a bracket in a location is always a range. `locations` is left out when the route has no variable segment and its only URL is the route itself.
- **`origin`** is the scheme and host every URL shares. Routes and locations are written without it. When the crawl covered several origins, `origin` is left out and routes and locations are full URLs.
- **Forms** are entries of their own: the form's method and action (the page itself when it has none) as the route, its fields in `params`, its encoding in `enctype` when it is not `application/x-www-form-urlencoded`, and no status, since katana does not submit them. The same form on 10 pages is one entry (9 duplicates).
- **No status** means katana recorded no response: katana writes an empty response for a URL whose body is identical to one it has already seen in the crawl, and for a request that failed (`error` is set then).
- **`via`** says where katana found the route when it was not a link in a page: `robots.txt`, `sitemap.xml` or `js`.
- **Static assets** (`.css`, `.js`, images, fonts, media, `.pdf`, `.map`) are one group per extension, route `*.css`, `*.js` and so on, never findings. katana leaves images and fonts out of its output by default.
- **Findings** are the routes that match a rule. Each rule has a fixed severity, and a finding takes the highest:

  | Tag | Severity | Rule |
  |---|---|---|
  | `file` | high, or low when it answered something other than 2xx | The last segment has a backup, dump, config, archive, key or log extension (`.bak .old .orig .swp .sql .db .sqlite .dump .env .ini .conf .cfg .config .yml .yaml .toml .properties .log .zip .tar .gz .tgz .7z .rar .war .jar .pem .key .p12 .pfx ...`), ends in `~`, or a segment is a dotfile (`/.env`, `/.git/config`). |
  | `admin` | medium | A segment is an admin, debug, internal, private, config or backup area: `admin*`, `wp-admin`, `debug`, `internal`, `private`, `actuator`, `console`, `manage`, `phpmyadmin`, `config`, `settings`, `backup`, `setup`, `install`, `server-status`, `metrics`, `env`, `dashboard`, `swagger`, `api-docs`, `graphiql`, `phpinfo` and similar. |
  | `upload` | medium | A `multipart/form-data` form, or an `upload`, `import` or `attachments` segment. |
  | `login` | medium | A `password`, `passwd`, `pwd` (and similar) field, or a `login`, `signin`, `signup`, `register`, `auth`, `oauth`, `sso`, `saml`, `forgot*`, `reset*` or `password` segment. |
  | `api` | low | An `api`, `graphql`, `gql`, `rest`, `rpc`, `soap`, `odata` or `v1`/`v2`... segment, or a JSON or XML response. |
  | `params` | low | Query parameters or form fields: injection surface. |
  | `form` | low | An HTML form. |
  | `auth` | low | Status 401, 403 or 407. |
  | `error` | low | Status 5xx. |

  A 404 or 410 gets none of the path rules (`file`, `admin`, `upload`, `login`, `api`): the path is not there.
- **Groups** are the routes that match no rule (plain pages, 404s, redirects) and the static assets.
- **Ranking:** findings by severity, then confidence, then the number of tags, then the number of URLs, then route and `id`. Groups by the number of URLs, then route and `id`. The same input always gives the same order.
- **`id`** is the status (or `none`) and a short hash of the method, origin, route, status, type, encoding and error, so the same route has the same `id` in the next run.
- **`raw`** is katana's full JSONL, saved to disk. It holds the headers, bodies, content lengths, source pages and timestamps. Reading it costs no tokens until the agent opens it. `--raw <file>` picks the path.
- **Budget:** `--max-bytes` (16,000 by default) keeps the output bounded on huge crawls. It cuts in this order until the JSON fits:
  1. group locations (each keeps its `count` of URLs);
  2. groups;
  3. the locations of the lowest-ranked findings, each keeping one location plus its `count`;
  4. the lowest-ranked findings.

  Every cut is counted in `omitted`, `budget.truncated` is set, and `next` points to the raw file. `--max-items` caps the number of findings (0, the default, keeps all).
- **`schema_version`** is the first field of every result (currently `1`), so an agent can check which version of the Raijin Schema it is reading.
- **Exit codes:** 0 is ok, 1 is wrong usage (bad flag, missing `--scope`, no target in scope), 2 is a structured error with the JSON on stdout (invalid scope, a scope that names no single host to crawl, katana missing); 1 never prints JSON: the agent tells them apart by `errors[].kind`.
- **Errors** say what to try next. If katana is missing, the exit code is 2. An invalid scope file stops the run before katana starts: the exit code is 2 and `errors` has one entry with `kind: "scope"` per invalid line, with the line number and what to write instead.

Fields an agent can rebuild are left out to save tokens:

- `type` (the content type, without `; charset=...`) is left out when it is `text/html`, and is `none` when the server sent none.
- `status` is left out when katana recorded no response.
- `params`, `enctype`, `via`, `error` and `locations` are left out when empty (see above for `locations`).
- `count` appears only when the budget cut `locations`.
- `confidence` appears only when it is not `matched`; every crawl rule is `matched`.
- `origin`, `omitted`, `errors` and `next` appear only when they are not empty.
- `budget` appears only when something was cut.

The JSON is compact (one line) by default because indentation costs 25% to 67% more tokens in the benchmark; `--pretty` indents it. `--format table` prints a short human view of the same result; it lists finding locations but only counts group URLs.

The Raijin banner is **off by default**, so normal runs stay small. `--banner` prints it to **stderr** (never stdout, so the JSON is untouched), with the version taken from the binary. Colors are disabled when `NO_COLOR` is set or stderr is not a terminal.

## Flags

| Flag | Default | |
|---|---|---|
| `--scope` | (required) | scope file |
| `-u` / `-l` / `--input` | every host the scope names | one start URL (repeatable) / start URL file / existing katana JSONL |
| `--depth` | 3 | maximum crawl depth (katana `-d`); known files need at least 3 |
| `--js` | true | parse JavaScript for endpoints (katana `-jc -jsl`) |
| `--known-files` | all | `all`, `robotstxt`, `sitemapxml` or `none` (katana `-kf`) |
| `--duration` | | maximum crawl time per target, e.g. `5m` (katana `-ct`) |
| `--rate`, `--timeout`, `--concurrency` | | katana `-rl` / `-timeout` / `-c` |
| `--max-items` | 0 | max findings (0 keeps all) |
| `--max-bytes` | 16000 | max JSON size; cuts are counted in `omitted` |
| `--raw` | temp file | where to save katana's full JSONL |
| `--state` | | state file for new-since-last-run |
| `--format` | json | `json` or `table` |
| `--pretty` | false | indent the JSON |
| `--katana` | katana | path to the binary |
| `--banner` | false | print the banner to stderr |

## Engine versions

The katana version raijin-crawl was tested with is in [engines.json](engines.json), and built into the binary. When the installed katana has another major version or is older, `errors` says so (`engine_version`, with the install command for the tested version); when katana writes lines without the fields raijin-crawl reads (`request.endpoint`, and a response or an error), `errors` says that too (`engine_format`) and the lines are counted in `omitted.unparsed`. Neither stops the run. A weekly GitHub Actions job runs the live benchmark with the latest katana and when it passes commits the bump to engines.json and tags a release, or an issue when it breaks.

## Development

```bash
go test ./...
go vet ./...
bench/run.sh            # replay bench/fixtures and count tokens
LIVE=1 bench/run.sh     # start the local lab, crawl it again, refresh the fixtures
```

The only dependency is [raijin-core](https://github.com/protonnegativo/raijin-core) (scope, schema envelope, state); the rest is the standard library. katana runs as a subprocess with `-jsonl -silent -nc -duc -or -fx`, so the binary stays small and katana can be updated on its own. Its stdin is `/dev/null`, because katana reads targets from stdin when stdin is not a terminal.

## Responsible use

Only crawl what you are authorized to test.
