"""Checks that every katana result is in a raijin-crawl result.

A result is one katana JSON line: its method and URL, the names of its query parameters, and what it
answered (status, content type without parameters, left out when text/html, and error). Each form on the
page is a result too: its method, action (the page URL when it has none), field names plus the action's
query parameter names, and encoding.

A result is present when a finding or group lists its URL (expanding "[1-5,7]" ranges and prepending
"origin" when the output has one), has the same method, status, type and error, has every one of its
parameter names in "params", and has a route the URL matches ({id}, {uuid} and {hash} are wildcards,
"*.ext" matches the extension). A form must be in an entry tagged "form" with the same encoding.
raijin-crawl writes a URL's own "[" and "]" as %5B and %5D, so the check does too.

usage: python verify.py <katana.jsonl> <raijin.json>
"""
import json, re, sys
from urllib.parse import unquote_plus

raw, out = sys.argv[1], sys.argv[2]
o = json.load(open(out))
origin = o.get("origin", "")


def origin_of(u):
    i = u.find("://")
    if i < 0:
        return ""
    m = re.search(r"[/?#]", u[i + 3:])
    return u[: i + 3 + m.start()] if m else u


def query_names(u):
    if "?" not in u:
        return []
    q = u.split("?", 1)[1].split("#", 1)[0]
    names = []
    for kv in q.split("&"):
        n = kv.split("=", 1)[0]
        try:
            n = unquote_plus(n, errors="strict")
        except UnicodeDecodeError:
            pass
        if n:
            names.append(n)
    return names


def ctype(ct, status):
    ct = (ct or "").split(";", 1)[0].strip().lower()
    if ct == "text/html":
        return ""
    if not ct and status:
        return "none"
    return ct


def expand(loc):
    m = re.search(r"\[([0-9,\-]+)\]", loc)
    if not m:
        return [loc]
    out = []
    for part in m.group(1).split(","):
        a, _, b = part.partition("-")
        for n in range(int(a), int(b or a) + 1):
            out.append(loc[: m.start()] + str(n) + loc[m.end():])
    return out


def route_regex(route):
    if route.startswith("*."):
        return None
    parts = re.split(r"(\{id\}|\{uuid\}|\{hash\})", route)
    wild = {"{id}": "[0-9]+", "{uuid}": "[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}",
            "{hash}": "[0-9a-fA-F]{16,}"}
    return re.compile("^" + "".join(wild.get(p, re.escape(p)) for p in parts) + "$")


def matches_route(url, route):
    path = url if not origin else url[len(origin):]
    if not origin:
        o_ = origin_of(url)
        if not route.startswith(o_):
            return False
        route, path = route[len(o_):], url[len(o_):]
    path = re.split(r"[?#]", path, maxsplit=1)[0] or "/"
    if route.startswith("*."):
        return path.lower().endswith(route[1:])
    return bool(route_regex(route).match(path))


# index: absolute URL -> list of entries
index = {}
for kind in ("findings", "groups"):
    for x in o[kind]:
        route = x["route"]
        method, _, rest = route.partition(" ")
        if not rest:
            method, rest = "GET", route
        x["_method"], x["_route"] = method, rest
        locs = x.get("locations")
        if locs is None:
            locs = [rest]
        for loc in locs:
            for u in expand(loc):
                index.setdefault(origin + u, []).append(x)


def present(method, url, status, ct, err, params, form=False, enctype=""):
    key = url.replace("[", "%5B").replace("]", "%5D")
    for x in index.get(key, []):
        if x["_method"] != method or x.get("status", 0) != status or x.get("type", "") != ct or x.get("error", "") != err:
            continue
        if not set(params) <= set(x.get("params", [])):
            continue
        if form and ("form" not in x.get("tags", []) or x.get("enctype", "") != enctype):
            continue
        if not form and "form" in x.get("tags", []):
            continue
        if matches_route(key, x["_route"]):
            return True
    return False


miss, n, forms = [], 0, 0
for line in open(raw):
    line = line.strip()
    if not line.startswith("{"):
        continue
    e = json.loads(line)
    req = e.get("request") or {}
    url = req.get("endpoint")
    if not url:
        continue
    n += 1
    resp = e.get("response") or {}
    status = resp.get("status_code") or 0
    headers = {k.lower(): v for k, v in (resp.get("headers") or {}).items()}
    ct = headers.get("content-type", "")
    if isinstance(ct, list):
        ct = ct[0] if ct else ""
    method = (req.get("method") or "GET").upper()
    if not present(method, url, status, ctype(str(ct), status), (e.get("error") or "").strip(), query_names(url)):
        miss.append((method, url, status))
    for f in resp.get("forms") or []:
        forms += 1
        action = f.get("action") or url
        enctype = (f.get("enctype") or "").strip().lower()
        if enctype == "application/x-www-form-urlencoded":
            enctype = ""
        params = query_names(action) + list(f.get("parameters") or [])
        if not present((f.get("method") or "GET").upper(), action, 0, "", "", params, True, enctype):
            miss.append(("form " + (f.get("method") or "GET").upper(), action, params))

print(f"{n} katana results and {forms} forms, {len(miss)} missing from output", "truncated" if o.get("budget") else "")
for m in miss[:5]:
    print("  MISSING", m)
sys.exit(1 if miss else 0)
