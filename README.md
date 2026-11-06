# golinks

[中文说明](README.zh-CN.md)

A single-machine **go links** service for macOS — the pattern used inside
many companies (`go/docs`, `go/gh`, …), rebuilt for one person and one Mac.

Type `go/gh` in your browser's address bar, get redirected to
`https://github.com`. Type `go/` to open a small admin page where you manage
the mappings. No cloud, no account, no database server: one static Go
binary, one JSON file, one `launchd` daemon.

## How it works

```
go/gh
  → /etc/hosts resolves "go" to 127.0.0.1
  → the golinks daemon (loopback only, ports 80/443) looks up "gh"
  → HTTP 302 to the mapped URL
```

* **hosts does the naming, the daemon does the redirecting.** A hosts entry
  alone can only map a name to an IP; the path lookup (`/gh` → URL) is the
  daemon's job.
* **302, never 301.** A 301 is cached hard by browsers, so editing a mapping
  would silently stop working. Every redirect here is a 302.
* **Exact match only.** There is deliberately *no* suffix passthrough:
  `go/gh/repo` does not forward `repo` to the target. Paths that don't match
  a key land on the admin page with the key prefilled for creation.
* **Loopback only.** The daemon binds `127.0.0.1` / `::1` and never
  `0.0.0.0`. That is why the admin page needs no login: nothing else on your
  network can reach it.

## Listener modes (HTTP / HTTPS are optional)

| Mode    | Port 80 | Port 443 | mkcert | Behaviour |
|---------|---------|----------|--------|-----------|
| `http`  | on      | off      | no     | Serves everything over plain HTTP. Simplest. |
| `both`  | on      | on       | yes    | HTTP only redirects to HTTPS. Recommended if you want HTTPS. |
| `https` | off     | on       | yes    | HTTPS only. |

HTTPS uses a [mkcert](https://github.com/FiloSottile/mkcert) local CA: the
root is installed only into *your* keychain, and the certificate it issues
for `go` is trusted by your browsers with no warnings. If you don't enable
HTTPS, mkcert is never installed or touched.

Invalid configurations fail loudly at startup — for example both listeners
disabled, or HTTPS enabled with a missing certificate — instead of silently
degrading.

## Install (macOS)

```sh
make dist            # or download a release binary into dist/
bash scripts/install.sh   # interactive mode choice
# or non-interactively:
bash scripts/install.sh --mode both
```

The installer:

1. installs the binary to `/usr/local/bin/golinks`
2. (HTTPS modes only) installs mkcert, creates the local CA, and issues a
   certificate for `go`
3. writes `/usr/local/etc/golinks/config.json`
4. adds `127.0.0.1 go` / `::1 go` to `/etc/hosts` (skipped if present)
5. registers the `local.golinks` launchd daemon (starts at boot, restarts
   on crash)
6. verifies `http(s)://go/_admin/health`

Then open `go/` (or `https://go/`) and add your first link.

### Clash / TUN proxy note

If you run a TUN-mode proxy (Clash Verge Rev / Mihomo, etc.), its fake-ip
DNS can intercept the name `go` before the system resolver consults
`/etc/hosts`. Symptom: `go` resolves to a `198.18.x.x` address and the page
never loads. Fix in the proxy's config (Merge/extension profile):

```yaml
dns:
  hosts:
    go: 127.0.0.1
  fake-ip-filter:
    - go
```

## Configuration

`/usr/local/etc/golinks/config.json` — see
[`config.example.json`](config.example.json).

| Field           | Default | Meaning |
|-----------------|---------|---------|
| `http_enabled`  | `true`  | Serve/redirect on the HTTP port |
| `https_enabled` | `false` | Serve on the HTTPS port (requires mkcert certs) |
| `http_port`     | `80`    | HTTP listen port (loopback) |
| `https_port`    | `443`   | HTTPS listen port (loopback) |
| `cert_file`     | —       | Certificate PEM (HTTPS only), SAN must cover `go` |
| `key_file`      | —       | Private key PEM (HTTPS only) |
| `data_file`     | `links.json` | Where mappings are stored (JSON, atomic writes) |

Changing the mode later = edit this file (run the mkcert steps on first
HTTPS enable), restart the daemon
(`sudo launchctl kickstart -k system/local.golinks`). Your links are not
affected.

## Run without installing

```sh
make build
./golinks -config config.example.json   # defaults: HTTP on port 80
```

(Ports below 1024 need root on macOS; for a quick test, set `http_port` to
something like `8080` and visit `http://127.0.0.1:8080/`.)

## Uninstall

```sh
bash scripts/uninstall.sh            # keeps config + links
bash scripts/uninstall.sh --purge    # also deletes config, data and hosts entry
bash scripts/uninstall.sh --purge --remove-ca   # also removes the mkcert CA
```

## Development

```sh
make test   # unit tests
make vet
make dist   # darwin/arm64 + darwin/amd64 binaries in dist/
```

Standard library only — no dependencies.

## Security notes

* The daemon runs as root under launchd (it binds ports 80/443). It only
  serves a fixed set of routes and stores plain JSON; review `handlers.go`
  before installing — it's short.
* The mkcert root CA private key stays on your machine
  (`~/Library/Application Support/mkcert`). Treat it like a master key:
  anyone holding it can mint trusted certificates for any site *on devices
  that trust your CA*. Don't sync it to cloud storage or commit it anywhere.
  This repository's `.gitignore` excludes all `*.pem` files by default.
* Never use a leaked or third-party "public CA" private key to sign
  certificates — that is forgery of the public trust system, and the keys
  from the 2026 eMudhra incident are revoked and blocked by all major
  browsers anyway. A local CA you control is the legitimate tool for local
  names like `go`.

## License

MIT
