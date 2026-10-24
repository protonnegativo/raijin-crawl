"""Local lab for the raijin-crawl benchmark: small websites on 127.0.0.1, one process.

Each argument is port=kind:

  shop  a noisy catalog: 60 user profiles (/user/1..60), 40 products (/product?id=1..40, each with a
        reviews tab), 20 articles under a hash (/article/<sha1>), 10 posts under a UUID, tag pages,
        and 70 static assets (CSS, 31 JS files, images, fonts; katana skips images and fonts by
        default). Almost everything is the same template.
  app   a small web app with real attack surface: a login form (POST), an upload form (multipart, file
        input), a search form (GET), a JSON API under /api/v1 and /graphql, a JS file that references
        more API routes, robots.txt and sitemap.xml pointing at a backup, an .env, an old config and a
        debug endpoint, a 401, a 403 and a 500.

usage: python3 lab.py 19500=shop 19501=app      (LAB_LOG=1 logs each request to stderr)
"""
import hashlib
import os
import sys
import threading
import uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlsplit, parse_qs

PAGE = """<!DOCTYPE html>
<html><head><title>{title}</title>
<link rel="stylesheet" href="/static/css/site.css">
<script src="/static/js/site.js"></script></head>
<body><nav><a href="/">Home</a> <a href="/about">About</a> <a href="/contact">Contact</a></nav>
<h1>{title}</h1>
{body}
</body></html>
"""

ARTICLES = [hashlib.sha1(f"article-{i}".encode()).hexdigest() for i in range(1, 21)]
POSTS = [str(uuid.uuid5(uuid.NAMESPACE_URL, f"post-{i}")) for i in range(1, 11)]
TAGS = ["news", "sale", "tips", "guides", "events"]
CSS = [f"/static/css/{n}.css" for n in ("site", "theme", "print", "grid", "forms")]
IMGS = [f"/static/img/p{i}.png" for i in range(1, 26)] + [f"/static/img/banner{i}.jpg" for i in range(1, 6)]
CHUNKS = [f"/static/js/chunk-{i}.js" for i in range(1, 31)]
FONTS = [f"/static/fonts/{n}.woff2" for n in ("inter", "inter-bold", "mono", "serif", "icons")]


def links(paths):
    return "\n".join(f'<a href="{p}">{p}</a>' for p in paths)


def shop(path, query):
    if path == "/":
        body = links([f"/user/{i}" for i in range(1, 61)])
        body += links([f"/product?id={i}" for i in range(1, 41)])
        body += links([f"/article/{h}" for h in ARTICLES])
        body += links([f"/post/{u}" for u in POSTS])
        body += links([f"/tag/{t}" for t in TAGS])
        body += "".join(f'<img src="{p}">' for p in IMGS)
        body += "".join(f'<link rel="stylesheet" href="{p}">' for p in CSS)
        body += "".join(f'<script src="{p}"></script>' for p in CHUNKS)
        body += "".join(f'<link rel="preload" href="{p}" as="font">' for p in FONTS)
        return 200, "text/html", PAGE.format(title="Shop", body=body)
    if path in ("/about", "/contact"):
        return 200, "text/html", PAGE.format(title=path[1:].title(), body="<p>Hello.</p>")
    if path.startswith("/user/") and path[6:].isdigit():
        return 200, "text/html", PAGE.format(title="User " + path[6:], body='<p>Profile.</p><img src="/static/img/p1.png">')
    if path == "/product" and query.get("id", [""])[0].isdigit():
        i = query["id"][0]
        body = f'<a href="/product?id={i}&tab=reviews">Reviews</a>'
        return 200, "text/html", PAGE.format(title="Product " + i, body=body)
    if path.startswith("/article/") and path[9:] in ARTICLES:
        return 200, "text/html", PAGE.format(title="Article", body="<p>Text.</p>")
    if path.startswith("/post/") and path[6:] in POSTS:
        return 200, "text/html", PAGE.format(title="Post", body="<p>Text.</p>")
    if path.startswith("/tag/") and path[5:] in TAGS:
        return 200, "text/html", PAGE.format(title="Tag", body=links(["/article/" + ARTICLES[0]]))
    return static(path)


def static(path):
    if path.endswith(".css"):
        return 200, "text/css", "body{margin:0}"
    if path.endswith(".js"):
        return 200, "application/javascript", "console.log(%r);" % path
    if path.endswith(".png"):
        return 200, "image/png", "\x89PNG"
    if path.endswith(".jpg"):
        return 200, "image/jpeg", "JFIF"
    if path.endswith(".woff2"):
        return 200, "font/woff2", "wOF2"
    return 404, "text/html", "<h1>Not Found</h1>"


APP_JS = """
const api = '/api/v1';
fetch('/api/v1/orders?status=open&page=1').then(r => r.json());
fetch('/api/v1/items/42');
fetch("/api/v2/admin/settings", {method: "POST"});
const debug = "/internal/debug";
"""

LOGIN = """<form method="POST" action="/login">
<input type="hidden" name="csrf_token" value="x">
<input type="text" name="username"><input type="password" name="password">
<input type="submit" value="Sign in"></form>"""

UPLOAD = """<form method="POST" action="/upload" enctype="multipart/form-data">
<input type="file" name="document"><input type="text" name="title">
<input type="submit" value="Upload"></form>"""

SEARCH = """<form method="GET" action="/search"><input type="text" name="q"><select name="sort"><option>new</option></select>
<input type="submit" value="Search"></form>"""


def app(path, query, method, host):
    if path == "/":
        body = links(["/login", "/upload", "/search?q=test", "/api/v1/users", "/api/v1/users/1", "/api/v1/users/2",
                      "/graphql", "/admin/", "/private/reports", "/error", "/download?file=report.pdf", "/docs",
                      "/user/1", "/user/2", "/user/3"])
        body += '<script src="/static/js/app.js"></script>' + SEARCH
        return 200, "text/html", PAGE.format(title="App", body=body)
    if path == "/robots.txt":
        return 200, "text/plain", "User-agent: *\nDisallow: /backup/db.sql\nDisallow: /config.old\nDisallow: /.env\nDisallow: /debug/vars\n"
    if path == "/sitemap.xml":
        urls = "".join(f"<url><loc>http://{host}{p}</loc></url>" for p in ("/docs", "/backup/site.zip", "/user/4"))
        return 200, "text/xml", f'<?xml version="1.0"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">{urls}</urlset>'
    if path == "/static/js/app.js":
        return 200, "application/javascript", APP_JS
    if path == "/login":
        if method == "POST":
            return 401, "text/html", "<h1>Invalid credentials</h1>"
        return 200, "text/html", PAGE.format(title="Sign in", body=LOGIN)
    if path == "/upload":
        return 200, "text/html", PAGE.format(title="Upload", body=UPLOAD)
    if path == "/search":
        return 200, "text/html", PAGE.format(title="Search", body=SEARCH)
    if path.startswith("/api/v1/users"):
        return 200, "application/json", '{"users":[{"id":1}]}'
    if path in ("/api/v1/orders", "/api/v1/items/42"):
        return 200, "application/json", "[]"
    if path == "/api/v2/admin/settings":
        return 403, "application/json", '{"error":"forbidden"}'
    if path == "/graphql":
        return 400, "application/json", '{"errors":[{"message":"Must provide query string."}]}'
    if path.startswith("/admin"):
        return 403, "text/html", "<h1>Forbidden</h1>"
    if path.startswith("/private"):
        return 401, "text/html", "<h1>Unauthorized</h1>"
    if path == "/error":
        return 500, "text/html", "<h1>Internal Server Error</h1>"
    if path == "/backup/db.sql":
        return 200, "application/sql", "CREATE TABLE users (id int);"
    if path == "/backup/site.zip":
        return 200, "application/zip", "PK"
    if path == "/.env":
        return 200, "text/plain", "APP_ENV=dev\n"
    if path == "/config.old":
        return 200, "text/plain", "debug=true\n"
    if path in ("/debug/vars", "/internal/debug"):
        return 200, "application/json", '{"cmdline":["app"]}'
    if path == "/download":
        return 200, "application/pdf", "%PDF-1.4"
    if path == "/docs":
        return 200, "text/html", PAGE.format(title="Docs", body="<p>Docs.</p>")
    if path.startswith("/user/") and path[6:].isdigit():
        return 200, "text/html", PAGE.format(title="User", body="<p>Profile.</p>")
    return static(path)


LOG = os.environ.get("LAB_LOG") == "1"


def handler(kind, port):
    class H(BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"

        def handle_any(self):
            u = urlsplit(self.path)
            q = parse_qs(u.query)
            if kind == "shop":
                code, ctype, body = shop(u.path, q)
            else:
                host = self.headers.get("Host", "127.0.0.1:%d" % port)
                code, ctype, body = app(u.path, q, self.command, host)
            data = body.encode("latin-1")
            self.send_response(code)
            self.send_header("Content-Type", ctype)
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            if self.command != "HEAD":
                self.wfile.write(data)

        do_GET = do_POST = do_HEAD = handle_any

        def log_message(self, fmt, *a):
            if LOG:
                sys.stderr.write("%d %s\n" % (port, fmt % a))

    return H


class Server(ThreadingHTTPServer):
    daemon_threads = True
    request_queue_size = 256

    def handle_error(self, request, client_address):
        pass  # a crawler closing a keep-alive connection is not an error


def main():
    servers = []
    for arg in sys.argv[1:]:
        port, kind = arg.split("=")
        s = Server(("127.0.0.1", int(port)), handler(kind, int(port)))
        servers.append(s)
        threading.Thread(target=s.serve_forever, daemon=True).start()
    print("lab up:", " ".join(sys.argv[1:]), flush=True)
    threading.Event().wait()


if __name__ == "__main__":
    main()
