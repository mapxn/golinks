package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testServer(t *testing.T, seed map[string]string) *server {
	t.Helper()
	path := filepath.Join(t.TempDir(), "links.json")
	st, err := loadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range seed {
		if err := st.Set(k, v, ""); err != nil {
			t.Fatal(err)
		}
	}
	return &server{store: st, cfg: defaultConfig()}
}

func doReq(s *server, method, target string, form url.Values) *httptest.ResponseRecorder {
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func TestRedirectFound(t *testing.T) {
	s := testServer(t, map[string]string{"gh": "https://github.com"})
	rec := doReq(s, "GET", "/gh", nil)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "https://github.com" {
		t.Fatalf("Location = %q, want https://github.com", loc)
	}
}

func TestRedirectCaseInsensitive(t *testing.T) {
	s := testServer(t, map[string]string{"gh": "https://github.com"})
	rec := doReq(s, "GET", "/GH", nil)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "https://github.com" {
		t.Fatalf("got %d %q", rec.Code, rec.Header().Get("Location"))
	}
}

func TestRedirectMissingGoesToAdminPrefilled(t *testing.T) {
	s := testServer(t, nil)
	rec := doReq(s, "GET", "/nope", nil)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/?missing=nope" {
		t.Fatalf("Location = %q, want /?missing=nope", loc)
	}
}

func TestNoSuffixPassthrough(t *testing.T) {
	s := testServer(t, map[string]string{"gh": "https://github.com"})
	rec := doReq(s, "GET", "/gh/some/repo", nil)
	loc := rec.Header().Get("Location")
	if strings.HasPrefix(loc, "https://github.com") {
		t.Fatalf("suffix was passed through: Location = %q", loc)
	}
	if loc != "/?missing=gh" {
		t.Fatalf("Location = %q, want /?missing=gh", loc)
	}
}

func TestAdminCRUD(t *testing.T) {
	s := testServer(t, nil)
	// Create
	rec := doReq(s, "POST", "/_admin/links", url.Values{"key": {"mail"}, "target": {"https://mail.example.com"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create status = %d", rec.Code)
	}
	// Redirect now resolves
	rec = doReq(s, "GET", "/mail", nil)
	if loc := rec.Header().Get("Location"); loc != "https://mail.example.com" {
		t.Fatalf("after create, Location = %q", loc)
	}
	// Update
	doReq(s, "POST", "/_admin/links", url.Values{"key": {"mail"}, "target": {"https://mail2.example.com"}})
	rec = doReq(s, "GET", "/mail", nil)
	if loc := rec.Header().Get("Location"); loc != "https://mail2.example.com" {
		t.Fatalf("after update, Location = %q (302 must not be stale)", loc)
	}
	// Delete
	doReq(s, "POST", "/_admin/delete", url.Values{"key": {"mail"}})
	rec = doReq(s, "GET", "/mail", nil)
	if loc := rec.Header().Get("Location"); loc != "/?missing=mail" {
		t.Fatalf("after delete, Location = %q", loc)
	}
}

func TestAdminPageShowsMissingBanner(t *testing.T) {
	s := testServer(t, nil)
	rec := doReq(s, "GET", "/?missing=wiki", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "go/wiki") {
		t.Fatal("admin page does not mention the missing key")
	}
}

func TestAdminSavedToastIsFloatingAndAutoDismisses(t *testing.T) {
	s := testServer(t, nil)
	rec := doReq(s, "GET", "/?saved=gh", nil)
	body := rec.Body.String()
	for _, want := range []string{
		`id="toasts"`,         // fixed overlay container: no layout shift
		`class="toast saved"`, // the saved notice is a toast now
		`class="close"`,       // manual close button
		`position: fixed`,     // overlay positioning in CSS
		`}, 3000);`,           // success toast auto-dismisses after 3s
	} {
		if !strings.Contains(body, want) {
			t.Errorf("admin page missing %q", want)
		}
	}
}

func TestAdminPageNumberingAndRowLayout(t *testing.T) {
	s := testServer(t, map[string]string{"aaa": "https://a.example.com", "bbb": "https://b.example.com"})
	rec := doReq(s, "GET", "/", nil)
	body := rec.Body.String()
	for _, want := range []string{`<td class="num">1</td>`, `<td class="num">2</td>`, `class="target-row"`} {
		if !strings.Contains(body, want) {
			t.Errorf("admin page missing %q", want)
		}
	}
	// In each row the controls must appear in Save, Delete, Open order.
	saveIdx := strings.Index(body, ">Save<")
	delIdx := strings.Index(body, ">Delete<")
	openIdx := strings.Index(body, ">Open<")
	if !(saveIdx >= 0 && delIdx > saveIdx && openIdx > delIdx) {
		t.Errorf("row controls out of order: save=%d delete=%d open=%d", saveIdx, delIdx, openIdx)
	}
}

func TestInvalidSaveRejected(t *testing.T) {
	s := testServer(t, nil)
	rec := doReq(s, "POST", "/_admin/links", url.Values{"key": {"bad key!"}, "target": {"https://x.example.com"}})
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/?error=") {
		t.Fatalf("invalid key accepted, Location = %q", loc)
	}
	rec = doReq(s, "POST", "/_admin/links", url.Values{"key": {"ok"}, "target": {"ftp://x.example.com"}})
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/?error=") {
		t.Fatalf("non-http target accepted, Location = %q", loc)
	}
}

func TestStorePersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "links.json")
	st, err := loadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Set("gh", "https://github.com", "code hosting"); err != nil {
		t.Fatal(err)
	}
	st2, err := loadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if link, ok := st2.Get("gh"); !ok || link.Target != "https://github.com" || link.Note != "code hosting" {
		t.Fatalf("reloaded store lost data: %+v %v", link, ok)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyDataFileStillLoads(t *testing.T) {
	// v1 format: {"links": {"old": "https://..."}} must keep working so an
	// upgrade never loses an existing install's mappings.
	path := filepath.Join(t.TempDir(), "links.json")
	if err := os.WriteFile(path, []byte(`{"links":{"old":"https://old.example.com"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	st, err := loadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	link, ok := st.Get("old")
	if !ok || link.Target != "https://old.example.com" || link.Note != "" {
		t.Fatalf("legacy entry = %+v, %v", link, ok)
	}
	// Saving upgrades the file to the current format.
	if err := st.Set("new", "https://new.example.com", ""); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `"target"`) {
		t.Fatalf("file not upgraded to current format: %s", data)
	}
}

func TestNoteIsOptionalAndShown(t *testing.T) {
	s := testServer(t, nil)
	doReq(s, "POST", "/_admin/links", url.Values{"key": {"gm"}, "target": {"https://mail.google.com"}, "note": {"邮箱"}})
	rec := doReq(s, "GET", "/", nil)
	if !strings.Contains(rec.Body.String(), "邮箱") {
		t.Fatal("note not shown on admin page")
	}
	// A mapping without a note works fine too.
	rec = doReq(s, "POST", "/_admin/links", url.Values{"key": {"nn"}, "target": {"https://n.example.com"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create without note: status = %d", rec.Code)
	}
}

func TestFaviconServed(t *testing.T) {
	s := testServer(t, nil)
	for _, p := range []string{"/favicon.svg", "/favicon.ico"} {
		rec := doReq(s, "GET", p, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d", p, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "image/svg+xml") {
			t.Fatalf("GET %s: Content-Type = %q", p, ct)
		}
		if !strings.Contains(rec.Body.String(), "<svg") {
			t.Fatalf("GET %s: body is not an SVG", p)
		}
	}
	rec := doReq(s, "GET", "/", nil)
	if !strings.Contains(rec.Body.String(), `rel="icon"`) {
		t.Fatal("admin page does not reference the favicon")
	}
}

func TestConfigValidation(t *testing.T) {
	c := defaultConfig()
	c.HTTPEnabled, c.HTTPSEnabled = false, false
	if err := c.validate(); err == nil {
		t.Fatal("both listeners disabled must be rejected")
	}

	c = defaultConfig()
	c.HTTPSEnabled = true // no cert files configured
	if err := c.validate(); err == nil {
		t.Fatal("https without cert/key must be rejected")
	}

	c = defaultConfig()
	c.HTTPSEnabled = true
	c.CertFile = "/nonexistent/cert.pem"
	c.KeyFile = "/nonexistent/key.pem"
	if err := c.validate(); err == nil {
		t.Fatal("https with missing cert files must be rejected")
	}

	c = defaultConfig()
	if err := c.validate(); err != nil {
		t.Fatalf("default config must be valid: %v", err)
	}
}

func TestHTTPSRedirectHandler(t *testing.T) {
	h := httpsRedirectHandler(443)
	req := httptest.NewRequest("GET", "http://go/abc?x=1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "https://go/abc?x=1" {
		t.Fatalf("Location = %q, want https://go/abc?x=1", loc)
	}
}

func TestKeyAndTargetValidation(t *testing.T) {
	for _, k := range []string{"", "has space", "a/b", "_admin", "-lead", "UPPER ok?"} {
		if _, err := NormalizeKey(k); err == nil {
			t.Errorf("NormalizeKey(%q) unexpectedly ok", k)
		}
	}
	if k, err := NormalizeKey("Gh_2-x"); err != nil || k != "gh_2-x" {
		t.Errorf("NormalizeKey(Gh_2-x) = %q, %v", k, err)
	}
	for _, tg := range []string{"", "example.com", "ftp://x.com", "https://"} {
		if err := ValidateTarget(tg); err == nil {
			t.Errorf("ValidateTarget(%q) unexpectedly ok", tg)
		}
	}
	if err := ValidateTarget("https://example.com/path?q=1"); err != nil {
		t.Errorf("ValidateTarget valid URL rejected: %v", err)
	}
}
