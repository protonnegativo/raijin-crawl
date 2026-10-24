# Token benchmark: katana vs raijin-crawl

How many tokens does an AI agent spend reading the result of a crawl, and does it lose anything? This benchmark crawls a local lab with katana, counts the tokens in each output shape, and checks that every katana result is in the raijin-crawl output.

Reproduce: `bench/run.sh` replays the saved crawls in `bench/fixtures`. `LIVE=1 bench/run.sh` starts the lab, crawls it again and refreshes the fixtures first (about 80 seconds; set `KATANA=<path>` if katana is not on `PATH`). A live crawl does not give byte-identical fixtures: see "katana's empty responses" below.

## Setup

- **Lab:** `bench/lab.py`, one Python process serving two sites on `127.0.0.1`, crawled as `http://localhost:19500/` and `http://localhost:19501/`. `localhost` and not `127.0.0.1`, because with an IP start URL and `-fs fqdn` katana 1.1.2 fetched `robots.txt` and `sitemap.xml` but followed none of their entries.
  - **noisy** (`shop`, port 19500): a catalog where almost every page is the same template. 60 user profiles (`/user/1` to `/user/60`), 40 products (`/product?id=N`), each with a reviews tab (`&tab=reviews`), 20 articles under a SHA-1 (`/article/<hash>`), 10 posts under a UUID, 5 tag pages, 5 CSS files, 31 JS files, and images and fonts, which katana leaves out of its output by default.
  - **findings** (`app`, port 19501): a small app with a login form (POST, password field), an upload form (multipart), a search form (GET), a JSON API under `/api/v1`, `/graphql`, a JS file whose `fetch()` calls name more API routes (one of them a 403 admin route), `robots.txt` pointing at `/backup/db.sql`, `/.env`, `/config.old` and `/debug/vars`, `sitemap.xml` with 3 more URLs, a 401, a 403 and a 500.
- **katana:** v1.1.2.
- **raijin-crawl:** runs katana itself (`-jsonl -silent -nc -duc -or -fx -d 3 -jc -jsl -kf all -cs <host>`) and saves its JSONL as `raw`: that file is the fixture (`raw.jsonl`). Defaults apply: no limit on findings, and `--max-bytes 16000`, which was not reached. All raijin-crawl rows replay `raw.jsonl` with `--input`, so they see the same crawl.
- **Baselines**, as an agent would run katana, with the same crawl options (`-d 3 -jc -jsl -kf all -fx` and the same crawl scope):
  - `katana -jsonl -silent`: katana's default JSONL, with the raw request and response strings and the body. A separate live crawl, saved as `katana.jsonl`; it found the same number of results.
  - `katana -jsonl` with raijin-crawl's flags (`-or`, body kept): the `raw` file.
  - `katana -jsonl`, no body: the `raw` file with `response.body` removed, which is what `-or -ob` writes. It is derived from `raw` instead of crawled with `-ob`, because katana 1.1.2 finds no endpoints in JS files when it omits the bodies, so a real `-ob` crawl would have fewer results.
  - Text: katana's default output, one URL per line, stdout and stderr (banner and log lines). A separate live crawl, saved as `text.txt` and `text_stderr.txt`; it found the same number of URLs.
- **Tokens:** counted with tiktoken (`o200k_base`). This is a **proxy**, because Claude's tokenizer is not public. `bench/count_tokens.py` also gives exact Claude counts if `ANTHROPIC_API_KEY` is set and the `anthropic` package is installed. `cl100k_base` gives the same ratios.

## Scenario 1: noisy

215 URLs. raijin-crawl returned 19 routes: 2 findings (both `/product` routes, `params`, low) and 17 groups (`/user/{id}` with 60 URLs in one location, `*.js` with 31, `/article/{hash}`, `/post/{uuid}`, `*.css`, the tag pages and the plain pages).

| Output | Bytes | Tokens | vs. katana -jsonl |
|---|---:|---:|---:|
| katana `-jsonl` | 306,719 | 115,723 | 1× |
| katana `-jsonl` with raijin-crawl's flags | 153,352 | 55,443 | 2.1× |
| katana `-jsonl`, no body | 72,367 | 23,158 | 5.0× |
| katana text (stdout+stderr) | 9,458 | 3,237 | 36× |
| **raijin-crawl JSON (default)** | 3,155 | **1,350** | **86×** |
| raijin-crawl `--pretty` | 4,411 | 1,684 | 69× |
| raijin-crawl `--format table` | 647 | 232 | 499× |

## Scenario 2: findings

32 URLs and 4 forms (the search form is on two pages, with two different actions). raijin-crawl returned 31 routes: 21 findings and 10 groups.

- high: `/backup/db.sql`, `/config.old` and `/.env`, all from `robots.txt`;
- medium: the admin API from the JS file (403), `POST /login`, `POST /upload` (multipart), `/admin/` (403), `/debug/vars`, `/private/reports` (401), `/internal/debug` from the JS file, and the `/login` and `/upload` pages;
- low: the search form and the search URL, the API routes, `/download?file=`, `/graphql` (400) and `/error` (500).

| Output | Bytes | Tokens | vs. katana -jsonl |
|---|---:|---:|---:|
| katana `-jsonl` | 33,982 | 12,098 | 1× |
| katana `-jsonl` with raijin-crawl's flags | 17,546 | 5,855 | 2.1× |
| katana `-jsonl`, no body | 11,462 | 3,560 | 3.4× |
| katana text (stdout+stderr) | 1,329 | 396 | 31× |
| **raijin-crawl JSON (default)** | 3,710 | **1,154** | **10.5×** |
| raijin-crawl `--pretty` | 6,323 | 1,931 | 6.3× |
| raijin-crawl `--format table` | 1,389 | 473 | 26× |

In a live run (not a replay) the raijin-crawl JSON also has `raw`, the path of katana's saved JSONL: about 17 tokens.

## Nothing is lost

For each scenario, `bench/verify.py` checks every katana result (a line of the `raw` JSONL) against the raijin-crawl output. A result is its method and URL, the names of its query parameters, and what it answered: status, content type (without parameters) and error. Each form on a page is a result too: its method, action, field names and encoding. A result is present when a finding or group lists its URL (after expanding `[1-60]` ranges and prepending `origin`), has the same method, status, type and error, has every one of its parameter names in `params`, and has a route the URL matches. A form must be in an entry tagged `form` with the same encoding.

- Scenario 1: 215 results, 0 forms, **0 missing**.
- Scenario 2: 32 results, 4 forms, **0 missing**.

The check fails when it should: with one URL taken out of the `/user/[1-60]` range (`[1-59]`), it reports `/user/60` missing.

What the JSON leaves out stays in katana's JSONL, which raijin-crawl saves to disk and points to in `raw`: headers, bodies, content lengths, the page each URL was found on, the tag and attribute that linked it, and timestamps.

## Reading the numbers

- **Against `katana -jsonl`**, the structured output an agent would parse, raijin-crawl uses **86× fewer tokens** on the noisy crawl and **10.5× fewer** on the findings crawl. Even against the leanest JSONL katana can write (no raw strings, no bodies), it is **17×** and **3.1×** smaller. The gap grows with repetition: 60 user pages cost one route and one location.
- **Against katana's text output**, the result depends on the crawl:
  - noisy: **2.4× smaller** (3,237 → 1,350);
  - findings: **2.9× larger** (396 → 1,154).

  The text output is a bare URL list. When every route is different there is little to collapse, and raijin-crawl's status codes, types, parameter names, IDs, tags and severities cost more than the URLs alone. In exchange the agent gets what the text does not have at all: the status of each URL, the 4 forms (`POST /login` with a `password` field, a multipart `POST /upload`), where each endpoint came from (`robots.txt`, `sitemap.xml`, a JS file), and the `.env`, the SQL dump and the 401/403s ranked first.
- **The output stays bounded.** On a crawl too big for `--max-bytes`, raijin-crawl cuts the least important data first: group locations, then groups, then extra locations of low-ranked findings, then low-ranked findings. Every cut is counted in `omitted`, and `next` points to the raw file. Raw katana output has no upper bound.

## katana's empty responses

katana writes an empty response (`"response":{"headers":{}}`, no status) for a URL whose body is identical to one it already saw in the crawl. `/user/1` to `/user/4` in the findings lab serve the same page, so one of them gets a status and the others do not (in the fixture, `/user/4`, found in `sitemap.xml`), and which one wins changes from crawl to crawl. raijin-crawl keeps those URLs as an entry with no status rather than guess that they answered like the others. That is why the fixtures are saved: a live run gives the same URLs and forms but can move a URL between a route's "200" entry and its "no response" entry.

## How the default output got here

The token cost of the two choices that matter most, measured by rewriting the final output of both scenarios (o200k tokens):

| Locations | Scenario 1 | Scenario 2 | Lossless? |
|---|---:|---:|---|
| Full URLs, one per URL | 3,640 | 1,392 | yes |
| Without the shared `origin`, one per URL | 2,519 | 1,166 | yes |
| Full URLs, numbers as ranges | 1,641 | 1,365 | yes |
| **Without the shared `origin`, numbers as ranges** (current) | **1,350** | **1,154** | **yes** |

One thing changed before the first benchmark run, on hand-run crawls of the same lab: raijin-crawl dropped `-ob` from its katana flags once it was clear that `-ob` stops `-jsl` from finding the JS endpoints. Without it, the findings crawl had no `/api/v1/orders`, `/api/v1/items/42`, `/api/v2/admin/settings` or `/internal/debug`.
