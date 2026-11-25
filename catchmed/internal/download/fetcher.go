package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// DefaultUserAgent identifies CatchMe honestly instead of pretending to be a browser.
const DefaultUserAgent = "CatchMe/0.1 (+https://github.com/ALi3naTEd0/CatchMe)"

var (
	// ErrRangeNotSupported means the server answered a range request with the whole file.
	ErrRangeNotSupported = errors.New("download: server ignored the range request")
	// ErrRemoteChanged means the file on the server is no longer the one we started downloading.
	ErrRemoteChanged = errors.New("download: file changed on the server")
	// ErrStalled means no bytes arrived within Fetcher.IdleTimeout.
	ErrStalled = errors.New("download: no data received within the idle timeout")
)

// StatusError is an HTTP response with a status we cannot use.
type StatusError struct {
	Code       int
	Status     string
	RetryAfter time.Duration // from the Retry-After header; 0 if absent
}

func (e *StatusError) Error() string { return "download: server returned " + e.Status }

// Retryable reports whether trying again later can succeed: timeouts,
// rate limits and server errors yes; 404 or 403 no.
func (e *StatusError) Retryable() bool {
	return e.Code == http.StatusRequestTimeout || e.Code == http.StatusTooManyRequests || e.Code >= 500
}

func statusError(resp *http.Response) error {
	return &StatusError{Code: resp.StatusCode, Status: resp.Status, RetryAfter: retryAfter(resp.Header.Get("Retry-After"), time.Now())}
}

// retryAfter reads Retry-After, which is either seconds ("120") or an HTTP date.
func retryAfter(v string, now time.Time) time.Duration {
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil && t.After(now) {
		return t.Sub(now)
	}
	return 0
}

// Fetcher performs the HTTP side of a download. The zero value works.
type Fetcher struct {
	Client      *http.Client  // nil uses http.DefaultClient
	UserAgent   string        // "" uses DefaultUserAgent
	IdleTimeout time.Duration // abort when no bytes arrive for this long; 0 disables it
}

func (f *Fetcher) client() *http.Client {
	if f.Client != nil {
		return f.Client
	}
	return http.DefaultClient
}

func (f *Fetcher) get(ctx context.Context, rawURL string, header http.Header) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	ua := f.UserAgent
	if ua == "" {
		ua = DefaultUserAgent
	}
	req.Header.Set("User-Agent", ua)
	// Ask for the raw bytes: with compression the sizes and ranges stop matching the file.
	req.Header.Set("Accept-Encoding", "identity")
	return f.client().Do(req)
}

// watchdog cancels ctx with ErrStalled when kick is not called for IdleTimeout.
// It covers both waiting for the response headers and every read of the body,
// unlike a single timeout for the whole request, which also kills slow but healthy
// downloads. Always call stop when done.
func (f *Fetcher) watchdog(ctx context.Context) (wctx context.Context, kick, stop func()) {
	if f.IdleTimeout <= 0 {
		return ctx, func() {}, func() {}
	}
	wctx, cancel := context.WithCancelCause(ctx)
	t := time.AfterFunc(f.IdleTimeout, func() { cancel(ErrStalled) })
	kick = func() { t.Reset(f.IdleTimeout) }
	stop = func() {
		t.Stop()
		cancel(nil)
	}
	return wctx, kick, stop
}

// cause replaces the generic "context canceled" error with the reason the context
// ended: ErrStalled from the watchdog, or the caller's own cancellation.
func cause(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		if c := context.Cause(ctx); c != nil {
			return c
		}
	}
	return err
}

// progressWriter forwards writes and reports each one: it feeds the watchdog
// and the caller's progress counter.
type progressWriter struct {
	w          io.Writer
	kick       func()
	onProgress func(n int64)
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n, err := p.w.Write(b)
	if n > 0 {
		p.kick()
		if p.onProgress != nil {
			p.onProgress(int64(n))
		}
	}
	return n, err
}
