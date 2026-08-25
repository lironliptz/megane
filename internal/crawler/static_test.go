package crawler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStaticClient_FetchHTML(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body><p>ok</p></body></html>"))
	}))
	defer ts.Close()

	cfg := DefaultConfig()
	sc := NewStaticClient(cfg)
	html, err := sc.FetchHTML(context.Background(), ts.URL+"/x")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "<p>ok</p>") {
		t.Fatalf("unexpected body: %s", html)
	}
}

func TestUniqueFilenameSanitize(t *testing.T) {
	n := UniqueFilename("../../../etc/passwd", "http://a/b")
	if strings.Contains(n, "..") || strings.Contains(n, "/") {
		t.Fatal(n)
	}
}
