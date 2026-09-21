package httpx

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stubTransport answers requests without a socket.
//
// httptest.NewServer would be the obvious choice and cannot be used: the
// sandbox this is developed in refuses to bind a listening socket
// (listen tcp6 [::1]:0: bind: operation not permitted), the same restriction
// that stops react-browser's Playwright suite running locally. Skipping on
// that error would leave the retry policy unverified while the suite still
// reported green, so the transport is injected instead -- which also makes
// these tests hermetic and instant everywhere else.
type stubTransport struct {
	hits      int
	responses []stubResponse
}

type stubResponse struct {
	status int
	body   string
	err    error
}

func (s *stubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	i := s.hits
	s.hits++
	if i >= len(s.responses) {
		i = len(s.responses) - 1
	}
	r := s.responses[i]
	if r.err != nil {
		return nil, r.err
	}
	return &http.Response{
		StatusCode: r.status,
		Status:     http.StatusText(r.status),
		Body:       io.NopCloser(strings.NewReader(r.body)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func newTestClient(responses ...stubResponse) (*Client, *stubTransport, *[]time.Duration) {
	tr := &stubTransport{responses: responses}
	waits := &[]time.Duration{}
	c := New()
	c.Transport = tr
	c.Sleep = func(d time.Duration) { *waits = append(*waits, d) }
	return c, tr, waits
}

func TestGetJSONDecodes(t *testing.T) {
	c, _, _ := newTestClient(stubResponse{status: 200, body: `{"version":"3.47.0"}`})

	var out map[string]any
	if err := c.GetJSON(context.Background(), "https://registry.example/x", &out); err != nil {
		t.Fatal(err)
	}
	if out["version"] != "3.47.0" {
		t.Fatalf("out = %v", out)
	}
}

func TestGetJSONSendsTheToolkitUserAgent(t *testing.T) {
	var seen string
	c := New()
	c.Sleep = func(time.Duration) {}
	c.Transport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		seen = req.Header.Get("User-Agent")
		return &http.Response{
			StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`)),
			Header: make(http.Header), Request: req,
		}, nil
	})

	var out map[string]any
	if err := c.GetJSON(context.Background(), "https://x", &out); err != nil {
		t.Fatal(err)
	}
	if seen != UserAgent {
		t.Errorf("User-Agent = %q, want %q", seen, UserAgent)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestGetJSONRetriesA503 covers the ported tenacity policy.
func TestGetJSONRetriesA503(t *testing.T) {
	c, tr, waits := newTestClient(
		stubResponse{status: 503},
		stubResponse{status: 503},
		stubResponse{status: 200, body: `{"version":"1.0.0"}`},
	)

	var out map[string]any
	if err := c.GetJSON(context.Background(), "https://x", &out); err != nil {
		t.Fatal(err)
	}
	if tr.hits != 3 {
		t.Errorf("hits = %d, want 3", tr.hits)
	}
	if len(*waits) != 2 {
		t.Errorf("backoffs = %d, want 2", len(*waits))
	}
}

func TestGetJSONRetriesATransportError(t *testing.T) {
	c, tr, _ := newTestClient(
		stubResponse{err: errors.New("connection refused")},
		stubResponse{status: 200, body: `{"ok":true}`},
	)
	var out map[string]any
	if err := c.GetJSON(context.Background(), "https://x", &out); err != nil {
		t.Fatal(err)
	}
	if tr.hits != 2 {
		t.Errorf("hits = %d, want 2", tr.hits)
	}
}

func TestGetJSONGivesUpAfterFourAttempts(t *testing.T) {
	c, tr, _ := newTestClient(stubResponse{status: 502})

	var out map[string]any
	if err := c.GetJSON(context.Background(), "https://x", &out); err == nil {
		t.Fatal("expected an error")
	}
	if tr.hits != Attempts {
		t.Errorf("hits = %d, want %d", tr.hits, Attempts)
	}
}

// TestGetJSONDoesNotRetryA404 pins a deliberate departure from the Python.
// tenacity retried on any RequestException and raise_for_status makes every
// non-2xx one of those, so a missing package cost four attempts and three
// backoffs before reporting what the first response already said.
func TestGetJSONDoesNotRetryA404(t *testing.T) {
	c, tr, waits := newTestClient(stubResponse{status: 404})

	var out map[string]any
	if err := c.GetJSON(context.Background(), "https://x", &out); err == nil {
		t.Fatal("expected an error")
	}
	if tr.hits != 1 {
		t.Errorf("hits = %d, want 1 -- a 404 will not become a 200", tr.hits)
	}
	if len(*waits) != 0 {
		t.Errorf("backed off %d times before a permanent failure", len(*waits))
	}
}

func TestGetJSONDoesNotRetryAMalformedBody(t *testing.T) {
	c, tr, _ := newTestClient(stubResponse{status: 200, body: `not json`})

	var out map[string]any
	if err := c.GetJSON(context.Background(), "https://x", &out); err == nil {
		t.Fatal("expected an error")
	}
	if tr.hits != 1 {
		t.Errorf("hits = %d, want 1 -- a malformed body will not repair itself", tr.hits)
	}
}

func TestBackoffIsBoundedAndMatchesTenacity(t *testing.T) {
	// wait_exponential(multiplier=2, min=2, max=30): 2s, 4s, 8s, ... capped.
	for _, tc := range []struct {
		attempt int
		max     time.Duration
		want    time.Duration
	}{
		{1, backoffMaxJSON, 2 * time.Second},
		{2, backoffMaxJSON, 4 * time.Second},
		{3, backoffMaxJSON, 8 * time.Second},
		{10, backoffMaxJSON, 30 * time.Second},
		{10, backoffMaxFile, 60 * time.Second},
	} {
		if got := backoff(tc.attempt, tc.max); got != tc.want {
			t.Errorf("backoff(%d, %v) = %v, want %v", tc.attempt, tc.max, got, tc.want)
		}
	}
}

// TestDownloadIsIdempotentByExistence covers the cache check: a non-empty file
// already at dest is kept, so re-running a recipe does not re-fetch an archive.
func TestDownloadIsIdempotentByExistence(t *testing.T) {
	c, tr, _ := newTestClient(stubResponse{status: 200, body: "payload"})
	dest := filepath.Join(t.TempDir(), "nested", "archive.zip")
	ctx := context.Background()

	if _, err := c.Download(ctx, "https://x", dest, false); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Download(ctx, "https://x", dest, false); err != nil {
		t.Fatal(err)
	}
	if tr.hits != 1 {
		t.Errorf("hits = %d, want 1 -- the second call must hit the cache", tr.hits)
	}

	if _, err := c.Download(ctx, "https://x", dest, true); err != nil {
		t.Fatal(err)
	}
	if tr.hits != 2 {
		t.Errorf("hits = %d, want 2 -- force must re-fetch", tr.hits)
	}

	data, err := os.ReadFile(dest)
	if err != nil || string(data) != "payload" {
		t.Fatalf("dest = %q, err = %v", data, err)
	}
}

// TestDownloadLeavesNoTruncatedFileOnFailure matters because the cache check
// above trusts any non-empty file: a half-written one would be a permanent
// wrong answer that no later run corrects.
func TestDownloadLeavesNoTruncatedFileOnFailure(t *testing.T) {
	c, _, _ := newTestClient(stubResponse{status: 404})
	dest := filepath.Join(t.TempDir(), "archive.zip")

	if _, err := c.Download(context.Background(), "https://x", dest, false); err == nil {
		t.Fatal("expected an error")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Error("a failed download left a file the next run would treat as a cache hit")
	}
	if _, err := os.Stat(dest + ".tmp"); !os.IsNotExist(err) {
		t.Error("a failed download left its .tmp behind")
	}
}

func TestDownloadRetriesA500(t *testing.T) {
	c, tr, _ := newTestClient(
		stubResponse{status: 500},
		stubResponse{status: 200, body: "payload"},
	)
	dest := filepath.Join(t.TempDir(), "archive.zip")

	if _, err := c.Download(context.Background(), "https://x", dest, false); err != nil {
		t.Fatal(err)
	}
	if tr.hits != 2 {
		t.Errorf("hits = %d, want 2", tr.hits)
	}
}
