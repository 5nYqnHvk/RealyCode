package webtools

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunWebFetchPlainTextAndHeaders(t *testing.T) {
	var userAgent, accept string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgent = r.Header.Get("User-Agent")
		accept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("hello"))
	}))
	defer server.Close()

	res, err := RunWebFetch(context.Background(), server.URL, NewEgressPolicy("http", true))
	if err != nil {
		t.Fatal(err)
	}
	if res.URL != server.URL || res.Title != server.URL || res.Data != "hello" || res.MediaType != "text/plain" {
		t.Fatalf("result = %#v", res)
	}
	if !strings.Contains(userAgent, "RelayCodeWebTools") || !strings.Contains(accept, "text/html") {
		t.Fatalf("headers ua=%q accept=%q", userAgent, accept)
	}
}

func TestRunWebFetchHTMLTitleAndTruncation(t *testing.T) {
	body := `<html><title>Doc</title><body>` + strings.Repeat("x", maxFetchChars+20) + `</body></html>`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	res, err := RunWebFetch(context.Background(), server.URL, NewEgressPolicy("http", true))
	if err != nil {
		t.Fatal(err)
	}
	if res.Title != "Doc" {
		t.Fatalf("title = %q", res.Title)
	}
	if len(res.Data) > maxFetchChars {
		t.Fatalf("data length = %d", len(res.Data))
	}
}

func TestRunWebFetchFollowsRelativeRedirect(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/done", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("done"))
	}))
	defer server.Close()

	res, err := RunWebFetch(context.Background(), server.URL+"/start", NewEgressPolicy("http", true))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(res.URL, "/done") || res.Data != "done" {
		t.Fatalf("result = %#v", res)
	}
}

func TestRunWebFetchRejectsRedirectWithoutLocationAndTooManyRedirects(t *testing.T) {
	missing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusFound)
	}))
	defer missing.Close()
	_, err := RunWebFetch(context.Background(), missing.URL, NewEgressPolicy("http", true))
	if err == nil || !strings.Contains(err.Error(), "missing Location") {
		t.Fatalf("missing Location err = %v", err)
	}

	loop := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.String(), http.StatusFound)
	}))
	defer loop.Close()
	_, err = RunWebFetch(context.Background(), loop.URL, NewEgressPolicy("http", true))
	if err == nil || !strings.Contains(err.Error(), "maximum redirects") {
		t.Fatalf("redirect loop err = %v", err)
	}
}

func TestRunWebFetchReturnsUpstreamStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusTeapot)
	}))
	defer server.Close()
	_, err := RunWebFetch(context.Background(), server.URL, NewEgressPolicy("http", true))
	if err == nil || !strings.Contains(err.Error(), "web_fetch upstream 418") {
		t.Fatalf("err = %v", err)
	}
}

func TestEgressPolicyValidationEdges(t *testing.T) {
	policy := NewEgressPolicy("", false)
	if !policy.AllowedSchemes["http"] || !policy.AllowedSchemes["https"] {
		t.Fatalf("default schemes = %#v", policy.AllowedSchemes)
	}
	if _, err := policy.ValidateURL("example.com"); err == nil || !strings.Contains(err.Error(), "scheme and host") {
		t.Fatalf("missing scheme err = %v", err)
	}
	if err := policy.ValidateHost(""); err == nil || !strings.Contains(err.Error(), "host is empty") {
		t.Fatalf("empty host err = %v", err)
	}
	if err := policy.ValidateIP(net.ParseIP("8.8.8.8")); err != nil {
		t.Fatalf("public IP rejected: %v", err)
	}
}

func TestNewValidatedClientDialsValidatedIP(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(r.Host))
	}))
	server.Listener = listener
	server.Start()
	defer server.Close()

	client := newValidatedClient(server.Client(), "example.test", []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}})
	resp, err := client.Get(fmt.Sprintf("http://example.test:%d/", listener.Addr().(*net.TCPAddr).Port))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}

func TestIsRedirect(t *testing.T) {
	for _, status := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		if !isRedirect(status) {
			t.Fatalf("%d not redirect", status)
		}
	}
	if isRedirect(http.StatusOK) {
		t.Fatal("200 is redirect")
	}
}
