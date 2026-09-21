// Package httpx is the HTTP layer: a shared client, retrying JSON reads and
// resumable-by-existence downloads.
//
// The retry policy is ported from core/http.py's tenacity decorators rather
// than re-chosen. GitHub's API is polled unauthenticated here, so it is
// rate-limited, and loosening the policy would fail a sync that used to work.
package httpx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	// UserAgent identifies the toolkit to the registries it polls.
	UserAgent = "ichava-maintainer-toolkit (+https://github.com/ichava/maintainer-toolkit)"

	// DefaultTimeout and DefaultDownloadTimeout match core/http.py.
	DefaultTimeout         = 30 * time.Second
	DefaultDownloadTimeout = 300 * time.Second

	// Attempts and the backoff bounds are tenacity's:
	// stop_after_attempt(4), wait_exponential(multiplier=2, min=2, max=30)
	// for JSON, and max=60 for downloads.
	Attempts       = 4
	backoffFactor  = 2 * time.Second
	backoffMinWait = 2 * time.Second
	backoffMaxJSON = 30 * time.Second
	backoffMaxFile = 60 * time.Second
)

// Client performs the toolkit's HTTP requests.
type Client struct {
	// Transport is the round tripper both the JSON and download clients are
	// built on. Tests supply a stub here rather than standing up an
	// httptest.Server: the sandbox this is developed in refuses to bind a
	// listening socket, and a suite that silently skips there would be the
	// same shape of false green this estate has been bitten by before.
	Transport http.RoundTripper

	// Sleep is swapped by tests so the retry path can be exercised without
	// waiting out the real backoff.
	Sleep func(time.Duration)
}

// New returns a client with the shared timeout.
func New() *Client {
	return &Client{Sleep: time.Sleep}
}

func (c *Client) clientFor(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: c.Transport}
}

func (c *Client) sleep(d time.Duration) {
	if c.Sleep != nil {
		c.Sleep(d)
	} else {
		time.Sleep(d)
	}
}

// backoff returns the wait before the given attempt, counting from 1.
func backoff(attempt int, max time.Duration) time.Duration {
	wait := time.Duration(math.Pow(2, float64(attempt-1))) * backoffFactor
	if wait < backoffMinWait {
		wait = backoffMinWait
	}
	if wait > max {
		wait = max
	}
	return wait
}

// retryable reports whether a status is worth another attempt.
//
// Python retried on any RequestException, and raise_for_status makes every
// non-2xx one of those -- so a 404 cost four attempts and three backoffs before
// reporting what it knew immediately. This narrows retries to transport
// errors, 429 and 5xx. A deliberate improvement, not a port slip.
func retryable(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

// GetJSON fetches and decodes a JSON document.
func (c *Client) GetJSON(ctx context.Context, url string, out any) error {
	client := c.clientFor(DefaultTimeout)
	var lastErr error

	for attempt := 1; attempt <= Attempts; attempt++ {
		if attempt > 1 {
			c.sleep(backoff(attempt-1, backoffMaxJSON))
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", UserAgent)
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		body, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		if resp.StatusCode >= 400 {
			lastErr = fmt.Errorf("GET %s: %s", url, resp.Status)
			if retryable(resp.StatusCode) {
				continue
			}
			return lastErr
		}
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if err := json.Unmarshal(body, out); err != nil {
			// A malformed body will not repair itself on a retry.
			return fmt.Errorf("GET %s: decoding response: %w", url, err)
		}
		return nil
	}

	return fmt.Errorf("GET %s: after %d attempts: %w", url, Attempts, lastErr)
}

// Download fetches url to dest and returns dest.
//
// Idempotent by existence: a non-empty file already at dest is kept unless
// force is set. The write goes to a sibling .tmp and is renamed, so an
// interrupted download never leaves a truncated file that the next run would
// treat as a complete cache hit.
func (c *Client) Download(ctx context.Context, url, dest string, force bool) (string, error) {
	if !force {
		if info, err := os.Stat(dest); err == nil && info.Size() > 0 {
			return dest, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}

	client := c.clientFor(DefaultDownloadTimeout)
	var lastErr error

	for attempt := 1; attempt <= Attempts; attempt++ {
		if attempt > 1 {
			c.sleep(backoff(attempt-1, backoffMaxFile))
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return "", err
		}
		req.Header.Set("User-Agent", UserAgent)

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode >= 400 {
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("GET %s: %s", url, resp.Status)
			if retryable(resp.StatusCode) {
				continue
			}
			return "", lastErr
		}

		tmp := dest + ".tmp"
		f, err := os.Create(tmp)
		if err != nil {
			_ = resp.Body.Close()
			return "", err
		}
		_, copyErr := io.Copy(f, resp.Body)
		closeErr := f.Close()
		_ = resp.Body.Close()

		if copyErr != nil || closeErr != nil {
			_ = os.Remove(tmp)
			if lastErr = copyErr; lastErr == nil {
				lastErr = closeErr
			}
			continue
		}
		if err := os.Rename(tmp, dest); err != nil {
			return "", err
		}
		return dest, nil
	}

	return "", fmt.Errorf("GET %s: after %d attempts: %w", url, Attempts, lastErr)
}
