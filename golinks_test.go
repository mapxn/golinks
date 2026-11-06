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
		if err := st.Set(k, v); err != nil {
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
	if err := st.Set("gh", "https://github.com"); err != nil {
		t.Fatal(err)
	}
	st2, err := loadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := st2.Get("gh"); !ok || v != "https://github.com" {
		t.Fatalf("reloaded store lost data: %q %v", v, ok)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
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
