package download

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fault is what faultServer does with one download request (not the probe).
type fault struct {
	status      int           // reply with this status instead of the file
	retryAfter  string        // Retry-After header to send with status
	cutAfter    int           // send this many body bytes, then drop the connection
	ignoreRange bool          // answer the whole file with 200
	etag        string        // pretend the file now has this ETag
	delay       time.Duration // send the body in 4 KB pieces with this pause between them
}

// faultServer serves content with ranges like rangeServer, but asks decide
// what to do with the n-th download request (n starts at 1). The probe
// (bytes=0-0) is always answered correctly. It records how many download
// requests arrived and the highest number of them running at once.
type faultServer struct {
	*httptest.Server
	requests    atomic.Int64
	inFlight    atomic.Int64
	maxInFlight atomic.Int64
	bytesSent   atomic.Int64 // body bytes of download requests (not the probe)
}

func newFaultServer(t testing.TB, content []byte, etag string, decide func(n int64, r *http.Request) fault) *faultServer {
	t.Helper()
	fs := &faultServer{}
	var mu sync.Mutex
	fs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		currentETag := etag
		if r.Header.Get("Range") != "bytes=0-0" {
			n := fs.requests.Add(1)
			now := fs.inFlight.Add(1)
			defer fs.inFlight.Add(-1)
			mu.Lock()
			if now > fs.maxInFlight.Load() {
				fs.maxInFlight.Store(now)
			}
			mu.Unlock()

			f := decide(n, r)
			if f.status != 0 {
				if f.retryAfter != "" {
					w.Header().Set("Retry-After", f.retryAfter)
				}
				http.Error(w, http.StatusText(f.status), f.status)
				return
			}
			if f.ignoreRange {
				w.Header().Set("Content-Length", strconv.Itoa(len(content)))
				_, _ = w.Write(content)
				return
			}
			if f.etag != "" {
				currentETag = f.etag
			}
			w = &countWriter{ResponseWriter: w, n: &fs.bytesSent}
			if f.delay > 0 {
				w = &slowWriter{ResponseWriter: w, delay: f.delay, done: r.Context().Done()}
			}
			if f.cutAfter > 0 {
				w = &cutWriter{ResponseWriter: w, left: f.cutAfter}
			}
		}
		if currentETag != "" {
			w.Header().Set("Etag", currentETag)
		}
		http.ServeContent(w, r, "", modTime, bytes.NewReader(content))
	}))
	t.Cleanup(fs.Close)
	return fs
}

// cutWriter lets `left` bytes of body through and then drops the connection.
type cutWriter struct {
	http.ResponseWriter
	left int
}

func (c *cutWriter) Write(b []byte) (int, error) {
	if len(b) >= c.left {
		_, _ = c.ResponseWriter.Write(b[:c.left])
		c.ResponseWriter.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	}
	c.left -= len(b)
	return c.ResponseWriter.Write(b)
}

// countWriter adds every body byte it passes on to n.
type countWriter struct {
	http.ResponseWriter
	n *atomic.Int64
}

func (c *countWriter) Write(b []byte) (int, error) {
	n, err := c.ResponseWriter.Write(b)
	c.n.Add(int64(n))
	return n, err
}

func (c *countWriter) Flush() { c.ResponseWriter.(http.Flusher).Flush() }

// slowWriter sends the body in 4 KB pieces with a pause after each one, and
// stops as soon as the client goes away.
type slowWriter struct {
	http.ResponseWriter
	delay time.Duration
	done  <-chan struct{}
}

func (s *slowWriter) Write(b []byte) (int, error) {
	written := 0
	for len(b) > 0 {
		piece := b[:min(len(b), 4096)]
		n, err := s.ResponseWriter.Write(piece)
		written += n
		if err != nil {
			return written, err
		}
		s.Flush()
		b = b[n:]
		select {
		case <-time.After(s.delay):
		case <-s.done:
			return written, http.ErrAbortHandler
		}
	}
	return written, nil
}

func (s *slowWriter) Flush() { s.ResponseWriter.(http.Flusher).Flush() }

func ok(int64, *http.Request) fault { return fault{} }
