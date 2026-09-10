# Static hosting

Serving a built Cairndex tree from a web server you own, from Cloudflare Pages,
or from Hugo's `public/`. Which disk shape you are serving is decided in
[the deployment overview](../deployment.md); this is what to put in front of it.

## nginx or caddy on a mirror you own

```nginx
server {
    listen 80;
    root /srv/site/public;

    # Cairndex's index.html *is* the autoindex, and it is static.
    autoindex off;
    index index.html;

    # Artifacts, served from their own tree at the same prefix.
    location /bootstrap/ {
        alias /srv/artifacts/bootstrap/;
        try_files $uri $uri/index.html =404;
    }
}
```

### Serving metadata as text

Each listing links to the `_meta.yaml` or `.cairndex.yaml` that describes it, so a
reader can see why an entry is titled the way it is. Without a MIME type those
download instead of opening:

```nginx
types {
    text/plain  yaml yml cfg list ipxe;
}
```

### Optional: `?format=` on a server you own

Pure file selection, not computation — which is why it works on a static tree,
and why `?depth=3` cannot be added the same way.

```nginx
map $arg_format $cairndex_ext {
    default "index.html";
    csv     "index.csv";
    json    "index.json";
}
location / {
    autoindex off;
    try_files $uri $uri/$cairndex_ext $uri/index.html =404;
}
```

Available only where you control the server. The path variants always work.

## Cloudflare Pages

```toml
# wrangler.toml
pages_build_output_dir = "public"
```

Path variants work: `index.json`, `index.csv`, `tree.json`. `?format=` does
**not** — Cloudflare Pages `_redirects` cannot match on a query string. Use the
paths.

## Styled listings, which need Hugo

`present: styled` needs a theme, so it needs Hugo, and Hugo insists on writing
to its own `public/`. That is the one arrangement with a copy in it — but the
copy goes small into big, never the reverse:

```sh
cairndex build --config /srv/site/cairndex.yaml   # writes content/, ~68 KB
hugo --source /srv/site                    # renders public/
cp -R /srv/site/public/. /srv/mirror/      # deposits pages into the tree
```

Hugo never sees the artifacts. In `hugo` mode Cairndex writes one small `_index.md`
per directory plus `index.txt` and `SHA256SUMS`; the example site's `content/` is
68 KB for a tree Hugo never reads. Nothing in `public/` is a mirrored byte, so
depositing it is proportional to the number of directories, not to the size of
the repository.

If you would rather avoid the copy entirely, use `present: bare` and `direct`
mode. The bare listing is a real autoindex — no JavaScript, readable in `lynx` —
which is what most of a mirror should be anyway.

The two settings are not independent. The styled presenter *is* a Hugo template,
so `direct` mode can only render `bare`. Asking for `outputs: [html]` in `direct`
mode while `present:` is `styled` — which is the default — writes no HTML at all,
and Cairndex says so once per run:

```
no HTML written: the styled presenter needs mode: hugo; use present: bare to render HTML directly
```

The machine formats are unaffected; `index.json`, `index.csv`, `index.txt` and
`SHA256SUMS` are rendered in Go and do not depend on the presenter.

## When the indexed tree is not the web root

`Entry.path` is rooted at Cairndex's `root:`. That is the site root only when the
two coincide, and on a site that indexes a subtree they do not:

```yaml
root: ./static/_odds     # where the files sit
base_path: /_odds        # where the site serves them
```

Without `base_path` a file the site serves at `/_odds/mockups/x.html` is
published in `index.json` as `/mockups/x.html`, so every link a consumer renders
from it is a 404 — and nothing says so, because the JSON is internally
consistent. Set it whenever `root:` is not what the web server treats as `/`.

It applies to every producer and to the listing's own path, so breadcrumbs and
entries agree. An authored `manifest` path that is already a full URL is left
alone: it names somewhere else on purpose.

## A CDN that rewrites HTML breaks HTML checksums

Measured on Cloudflare Pages, on the same file at three stages:

```
source          38b11e8e   58,979 bytes
hugo public/    38b11e8e   58,979 bytes
production      8631d212   60,131 bytes
```

Cloudflare injects its Pages Analytics beacon and a bot-detection script before
`</body>`, so every HTML response is 1,152 bytes longer than the file Cairndex
hashed. `SHA256SUMS` is right and the served bytes are not the served file.

Non-HTML is untouched. PDFs, audio, archives and disk images fetched from the
same deployment verify normally — which is the case that matters, because those
are the artifacts anyone checksums. A mockup page is not.

Two settings cause it, and they are separate: Pages Web Analytics is a project
setting with no path scoping, and JavaScript Detections is a zone setting that a
Configuration Rule can disable for one path prefix. Turning off only one leaves
the other injecting. Weigh that against what it buys: on a mirror the binaries
already verify, and trading site-wide analytics and a bot signal to checksum
HTML is rarely worth it.
