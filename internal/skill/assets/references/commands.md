# The curated commands

Hand-written commands for the three entities most people touch, plus the core commands that
have nothing to do with the API. Everything else lives in
[discovery.md](discovery.md).

Run `--help` on any of them. It tells you the endpoint, the permission it needs, and a
sample body. The notes below are the things `--help` will *not* tell you.

## Facilities

A facility is addressed by its id.

```sh
fft facility list --status ACTIVE
fft facility get berlin-warehouse
fft facility create --example > facility.json
fft facility create --file facility.json
fft facility patch berlin-warehouse --name "Berlin Warehouse"
fft facility delete berlin-warehouse
fft facility coordinates set berlin-warehouse --lat 52.52 --lon 13.405
fft facility search --file query.json
```

- `patch` changes named fields; `update` replaces the whole object from a file. Prefer
  `patch` — it cannot silently drop a field you did not know about.
- `delete` is destructive and irreversible. Ask first, every time.

## Listings

**A listing has no id of its own.** It is addressed by the facility it is in *and* the
article it is for, so every listing command needs `--facility`:

```sh
fft listing list --facility berlin-warehouse
fft listing get --facility berlin-warehouse ARTICLE-123
fft listing patch --facility berlin-warehouse ARTICLE-123 --status ACTIVE
fft listing delete --facility berlin-warehouse ARTICLE-123
```

- `fft listing set --facility <id> --file listings.json` is a **PUT**: it replaces the
  facility's listings, all or nothing. It is not an upsert, and what is not in the file is
  gone.
- `fft listing upsert --file listings.json` is the bulk path, and the one you almost always
  want. It is chunked, and it reports per item — so it can exit 8 with some items written.
- `fft listing purge --facility <id>` deletes every listing in a facility. There is no
  narrower way to say "I meant that".

## Stock

Stock has an id of its own, but you will nearly always arrive at it through the facility and
the article.

```sh
fft stock list --facility berlin-warehouse --tenant-article-id ARTICLE-123
fft stock get 7f3ce2a4-1f00-4c6a-9a2b-5d1f0b7a1c33
fft stock summary --facility berlin-warehouse
fft stock upsert --file stocks.json
fft stock actions --file action.json
```

- `stock summary` answers "how much of this do we have" without paging every stock record.
  Reach for it before you reach for `list`.
- `stock actions` is how stock is *moved and changed* (pick, book in, correct) rather than
  overwritten. If the user is describing a real-world event, this is usually the endpoint.
- `stock upsert` is bulk and chunked; exit 8 means some items landed.

## Paging

Every list and search takes the same flags:

```sh
fft stock list --facility berlin-warehouse --size 100
fft stock list --facility berlin-warehouse --all --max-items 500
fft facility list --total
```

- The default is one page. `--all` follows the cursor, stopping at `--max-items` — which
  **defaults to 10,000**, so `--all` is bounded whether you say so or not. Lower it when the
  question is exploratory; a real tenant is a great many requests.
- Hitting that limit is **not an error**: fft warns on stderr and exits 0 with what it got,
  so the JSON alone cannot tell you the answer was cut short.
- `--total` asks the API for the count, and prints it **to stderr**, not into the JSON.

## Core commands

```sh
fft project list
fft project current
fft project use staging
fft project read-only prod
fft auth whoami
fft ping
fft version
```

- `fft auth whoami` prints the permissions the current credentials actually have. When
  something exits 5, this is the command that explains why.
- `fft ping` needs no credentials at all. It is the way to tell "the tenant is down" apart
  from "my token is wrong".
- `fft project read-only prod` marks a project read-only for good, in the config file. It is
  the right suggestion whenever a user tells you a project is production.
