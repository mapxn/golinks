package main

import (
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/url"
	"strings"
)

type server struct {
	store *Store
	cfg   Config
}

// faviconSVG is the site's icon: a blue rounded square with "go" in white.
// Served at /favicon.svg and /favicon.ico and referenced from the admin
// page's <link rel="icon">.
const faviconSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" rx="14" fill="#0a64ff"/><text x="32" y="44" font-family="Arial, Helvetica, sans-serif" font-size="30" font-weight="bold" fill="#ffffff" text-anchor="middle">go</text></svg>`

// ServeHTTP routes:
//
//	GET  /                  admin page (list / create / edit / delete)
//	POST /_admin/links      create or update a mapping (form: key, target)
//	POST /_admin/delete     delete a mapping (form: key)
//	GET  /_admin/health     "ok"
//	GET  /<key>             302 to the mapped target (exact match only)
//	GET  /<anything else>   302 to the admin page with the key prefilled
//
// There is deliberately no suffix passthrough (requirements v2):
// /gh/extra does NOT redirect to <target of gh>/extra.
func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/favicon.svg" || r.URL.Path == "/favicon.ico":
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=86400")
		fmt.Fprint(w, faviconSVG)
	case r.URL.Path == "/":
		s.handleAdmin(w, r)
	case r.URL.Path == "/_admin/links":
		s.handleSave(w, r)
	case r.URL.Path == "/_admin/delete":
		s.handleDelete(w, r)
	case r.URL.Path == "/_admin/health":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "ok")
	case strings.HasPrefix(r.URL.Path, "/_admin/"):
		http.NotFound(w, r)
	default:
		s.handleRedirect(w, r)
	}
}

func (s *server) handleRedirect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	trimmed := strings.Trim(r.URL.Path, "/")
	first := trimmed
	if i := strings.Index(trimmed, "/"); i >= 0 {
		// Extra path segments are never forwarded to the target.
		first = trimmed[:i]
		http.Redirect(w, r, "/?missing="+url.QueryEscape(strings.ToLower(first)), http.StatusFound)
		return
	}
	key, err := NormalizeKey(first)
	if err != nil {
		http.Redirect(w, r, "/?missing="+url.QueryEscape(strings.ToLower(first)), http.StatusFound)
		return
	}
	if link, ok := s.store.Get(key); ok {
		// 302, never 301: a 301 would be cached hard by browsers and a
		// later edit of the mapping would silently stop taking effect.
		http.Redirect(w, r, link.Target, http.StatusFound)
		return
	}
	http.Redirect(w, r, "/?missing="+url.QueryEscape(key), http.StatusFound)
}

// adminLink is a Link plus its 1-based row number for the admin table.
type adminLink struct {
	Num    int
	Key    string
	Target string
	Note   string
}

type adminData struct {
	Links   []adminLink
	Missing string
	Error   string
	Saved   string
}

var adminTmpl = template.Must(template.New("admin").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<link rel="icon" type="image/svg+xml" href="/favicon.svg">
<title>go links</title>
<style>
  body { font-family: -apple-system, system-ui, sans-serif; margin: 2rem auto; max-width: 1080px; padding: 0 1rem; color: #1c1c1e; }
  h1 { font-size: 1.4rem; }
  .banner { border-radius: 8px; padding: .7rem 1rem; margin: 1rem 0; }
  .missing { background: #fff4d6; border: 1px solid #e5c15c; }
  .error { background: #fde3e3; border: 1px solid #e58c8c; }
  .saved { background: #e2f5e3; border: 1px solid #8cc492; }
  table { width: 100%; border-collapse: collapse; margin-top: 1rem; }
  th, td { text-align: left; padding: .45rem .5rem; border-bottom: 1px solid #e3e3e6; vertical-align: middle; }
  td.num, th.num { width: 2.2rem; color: #888; }
  td.key { font-family: ui-monospace, monospace; white-space: nowrap; }
  input[type=text], input[type=url] { width: 100%; box-sizing: border-box; padding: .4rem .5rem; border: 1px solid #c9c9ce; border-radius: 6px; }
  .target-row { display: flex; gap: .5rem; align-items: center; }
  .target-row form.save { flex: 1; display: flex; gap: .5rem; align-items: center; margin: 0; }
  .target-row form.save input[name=target] { flex: 2; }
  .target-row form.save input[name=note] { flex: 1; }
  .target-row form.del { margin: 0; }
  .target-row button { white-space: nowrap; }
  a.btn { display: inline-block; padding: .4rem .8rem; border: 1px solid #c9c9ce; background: #f5f5f7; border-radius: 6px; color: #1c1c1e; text-decoration: none; white-space: nowrap; }
  button { padding: .4rem .8rem; border: 1px solid #c9c9ce; background: #f5f5f7; border-radius: 6px; cursor: pointer; }
  button.primary { background: #0a64ff; border-color: #0a64ff; color: #fff; }
  button.danger { color: #b00020; }
  .row { display: flex; gap: .6rem; align-items: end; }
  .row > div { flex: 1; }
  label { display: block; font-size: .8rem; color: #555; margin-bottom: .25rem; }
  .hint { color: #666; font-size: .85rem; }
</style>
</head>
<body>
<h1>go/ links</h1>
{{if .Missing}}
<div class="banner missing"><strong>go/{{.Missing}}</strong> does not exist yet. Create it below — the key is already filled in.</div>
{{end}}
{{if .Error}}
<div class="banner error">{{.Error}}</div>
{{end}}
{{if .Saved}}
<div class="banner saved">Saved <strong>go/{{.Saved}}</strong>.</div>
{{end}}

<form method="post" action="/_admin/links">
  <div class="row">
    <div style="flex:0 0 150px"><label for="key">Key (go/…)</label><input type="text" id="key" name="key" value="{{.Missing}}" placeholder="gh" required></div>
    <div style="flex:2"><label for="target">Target URL</label><input type="url" id="target" name="target" placeholder="https://example.com" required></div>
    <div style="flex:1"><label for="note">Note (optional)</label><input type="text" id="note" name="note" placeholder="What is this link for?"></div>
    <div style="flex:0 0 auto"><button class="primary" type="submit">Save</button></div>
  </div>
</form>

<input type="text" id="filter" placeholder="Filter links…" style="margin-top:1.2rem">
<p class="hint">{{len .Links}} link(s). Editing a target and saving takes effect immediately.</p>

<table id="links">
  <tr><th class="num">#</th><th style="width:160px">Key</th><th>Target · Note (optional)</th></tr>
  {{range .Links}}
  <tr class="link-row">
    <td class="num">{{.Num}}</td>
    <td class="key">{{.Key}}</td>
    <td>
      <div class="target-row">
        <form class="save" method="post" action="/_admin/links">
          <input type="hidden" name="key" value="{{.Key}}">
          <input type="url" name="target" value="{{.Target}}">
          <input type="text" name="note" value="{{.Note}}" placeholder="note (optional)">
          <button type="submit">Save</button>
        </form>
        <form class="del" method="post" action="/_admin/delete" onsubmit="return confirm('Delete go/{{.Key}}?')">
          <input type="hidden" name="key" value="{{.Key}}">
          <button class="danger" type="submit">Delete</button>
        </form>
        <a class="btn" href="/{{.Key}}">Open</a>
      </div>
    </td>
  </tr>
  {{end}}
</table>

<script>
  var f = document.getElementById('filter');
  f.addEventListener('input', function () {
    var q = f.value.toLowerCase();
    document.querySelectorAll('.link-row').forEach(function (row) {
      row.style.display = row.textContent.toLowerCase().includes(q) ? '' : 'none';
    });
  });
</script>
</body>
</html>`))

func (s *server) handleAdmin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	all := s.store.All()
	links := make([]adminLink, 0, len(all))
	for i, l := range all {
		links = append(links, adminLink{Num: i + 1, Key: l.Key, Target: l.Target, Note: l.Note})
	}
	data := adminData{
		Links:   links,
		Missing: q.Get("missing"),
		Error:   q.Get("error"),
		Saved:   q.Get("saved"),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := adminTmpl.Execute(w, data); err != nil {
		// Headers are already sent; nothing useful left to do but log via panic-free path.
		_ = err
	}
}

func redirectWithError(w http.ResponseWriter, r *http.Request, msg string) {
	http.Redirect(w, r, "/?error="+url.QueryEscape(msg), http.StatusSeeOther)
}

func (s *server) handleSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectWithError(w, r, "could not parse form")
		return
	}
	key := r.FormValue("key")
	target := strings.TrimSpace(r.FormValue("target"))
	note := r.FormValue("note")
	if err := s.store.Set(key, target, note); err != nil {
		redirectWithError(w, r, err.Error())
		return
	}
	normalized, _ := NormalizeKey(key)
	http.Redirect(w, r, "/?saved="+url.QueryEscape(normalized), http.StatusSeeOther)
}

func (s *server) handleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		redirectWithError(w, r, "could not parse form")
		return
	}
	if err := s.store.Delete(r.FormValue("key")); err != nil {
		redirectWithError(w, r, err.Error())
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// httpsRedirectHandler is used on the HTTP listener when HTTPS is also
// enabled: plain HTTP never serves content, it only upgrades the request to
// the same host and path over HTTPS.
func httpsRedirectHandler(httpsPort int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if httpsPort != 443 {
			host = net.JoinHostPort(host, fmt.Sprint(httpsPort))
		}
		target := "https://" + host + r.URL.RequestURI()
		http.Redirect(w, r, target, http.StatusFound)
	})
}
