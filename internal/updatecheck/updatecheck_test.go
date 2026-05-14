package updatecheck

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/5nYqnHvk/RelayCode/internal/config"
	"github.com/5nYqnHvk/RelayCode/internal/version"
)

func TestMaybeNotifySkipsDisabledAndDev(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"tag_name":"v9.9.9"}`))
	}))
	defer server.Close()

	withVersion(t, "v1.0.0")
	MaybeNotify(context.Background(), config.ServerConfig{UpdateCheckURL: server.URL, UpdateCheckTimeoutSeconds: 1})
	withVersion(t, "dev")
	MaybeNotify(context.Background(), config.ServerConfig{EnableUpdateNotification: true, UpdateCheckURL: server.URL, UpdateCheckTimeoutSeconds: 1})
	time.Sleep(20 * time.Millisecond)
	if requests != 0 {
		t.Fatalf("requests = %d", requests)
	}
}

func TestMaybeNotifyLogsNewerRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != "RelayCodeUpdateCheck" {
			t.Fatalf("User-Agent = %q", got)
		}
		_, _ = w.Write([]byte(`{"tag_name":"v1.3.0"}`))
	}))
	defer server.Close()
	withVersion(t, "v1.2.0")
	var buf bytes.Buffer
	restore := captureLog(&buf)
	defer restore()

	notify(context.Background(), server.URL, "v1.2.0")
	if !strings.Contains(buf.String(), "update available") || !strings.Contains(buf.String(), "current=v1.2.0 latest=v1.3.0") {
		t.Fatalf("log = %q", buf.String())
	}
}

func TestMaybeNotifySuppressesErrorsAndOlderTags(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		code int
	}{
		{"older", `{"tag_name":"v1.1.9"}`, http.StatusOK},
		{"equal", `{"tag_name":"v1.2.0"}`, http.StatusOK},
		{"malformed", `{"tag_name":"new"}`, http.StatusOK},
		{"bad json", `{`, http.StatusOK},
		{"server error", `{"tag_name":"v9.9.9"}`, http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.code)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			withVersion(t, "v1.2.0")
			var buf bytes.Buffer
			restore := captureLog(&buf)
			defer restore()

			notify(context.Background(), server.URL, "v1.2.0")
			if buf.Len() != 0 {
				t.Fatalf("log = %q", buf.String())
			}
		})
	}
}

func TestFetchLatestTag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v2.0.0"}`))
	}))
	defer server.Close()
	tag, err := fetchLatestTag(context.Background(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if tag != "v2.0.0" {
		t.Fatalf("tag = %q", tag)
	}
}

func TestVersionComparison(t *testing.T) {
	for _, tc := range []struct {
		latest  string
		current string
		want    bool
	}{
		{"v1.2.4", "v1.2.3", true},
		{"v1.3.0", "v1.2.9", true},
		{"v2.0.0", "v1.9.9", true},
		{"v1.2.3", "v1.2.3", false},
		{"v1.2.2", "v1.2.3", false},
		{"1.2.4", "v1.2.3", true},
		{"v1.2", "v1.2.3", false},
		{"v1.2.3-beta", "v1.2.2", false},
		{"v1.2.3", "dev", false},
	} {
		if got := isNewer(tc.latest, tc.current); got != tc.want {
			t.Fatalf("isNewer(%q,%q) = %v, want %v", tc.latest, tc.current, got, tc.want)
		}
	}
}

func withVersion(t *testing.T, value string) {
	t.Helper()
	old := version.Version
	version.Version = value
	t.Cleanup(func() { version.Version = old })
}

func captureLog(buf *bytes.Buffer) func() {
	oldWriter := log.Writer()
	oldFlags := log.Flags()
	log.SetOutput(buf)
	log.SetFlags(0)
	return func() {
		log.SetOutput(oldWriter)
		log.SetFlags(oldFlags)
	}
}
