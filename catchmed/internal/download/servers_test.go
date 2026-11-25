package download

import (
	"bytes"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// Test servers: each one imitates a kind of real server CatchMe will meet.
// All run locally with httptest, so the tests need no internet.

var modTime = time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

// testContent returns n reproducible pseudo-random bytes, so a byte out of
// place anywhere shows up as a mismatch.
func testContent(n int) []byte {
	b := make([]byte, n)
	r := rand.New(rand.NewPCG(42, uint64(n)))
	for i := range b {
		b[i] = byte(r.Uint32())
	}
	return b
}

// rangeServer is a well-behaved server: ranges, ETag and If-Range, as
// implemented by net/http.ServeContent.
func rangeServer(t testing.TB, content []byte, etag, disposition string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if etag != "" {
			w.Header().Set("Etag", etag)
		}
		if disposition != "" {
			w.Header().Set("Content-Disposition", disposition)
		}
		http.ServeContent(w, r, "", modTime, bytes.NewReader(content))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// noRangeServer ignores Range and always sends the whole file with 200.
func noRangeServer(t testing.TB, content []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		_, _ = w.Write(content)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// unknownSizeServer streams the file without Content-Length (chunked encoding),
// like a generated export or a live stream.
func unknownSizeServer(t testing.TB, content []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		half := len(content) / 2
		_, _ = w.Write(content[:half])
		w.(http.Flusher).Flush() // sends headers without a length
		_, _ = w.Write(content[half:])
	}))
	t.Cleanup(srv.Close)
	return srv
}

// statusServer answers every request with the given status.
func statusServer(t *testing.T, code int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, http.StatusText(code), code)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// stallServer sends the headers and `sent` bytes of a 206, then goes silent
// until the client gives up. sent = -1 stalls before sending any headers.
func stallServer(t testing.TB, content []byte, sent int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sent >= 0 {
			w.Header().Set("Content-Range", "bytes 0-"+strconv.Itoa(len(content)-1)+"/"+strconv.Itoa(len(content)))
			w.Header().Set("Content-Length", strconv.Itoa(len(content)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(content[:sent])
			w.(http.Flusher).Flush()
		}
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	return srv
}

// slowServer sends a full 206 in pieces of `piece` bytes with `every` between them:
// slow overall, but never silent for long.
func slowServer(t testing.TB, content []byte, piece int, every time.Duration) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Range", "bytes 0-"+strconv.Itoa(len(content)-1)+"/"+strconv.Itoa(len(content)))
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.WriteHeader(http.StatusPartialContent)
		for off := 0; off < len(content); off += piece {
			_, _ = w.Write(content[off:min(off+piece, len(content))])
			w.(http.Flusher).Flush()
			select {
			case <-time.After(every):
			case <-r.Context().Done():
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// cutServer promises the whole range, sends `sent` bytes and drops the connection.
func cutServer(t testing.TB, content []byte, sent int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Range", "bytes 0-"+strconv.Itoa(len(content)-1)+"/"+strconv.Itoa(len(content)))
		w.Header().Set("Content-Length", strconv.Itoa(len(content)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(content[:sent])
		w.(http.Flusher).Flush()
		panic(http.ErrAbortHandler) // closes the connection without logging
	}))
	t.Cleanup(srv.Close)
	return srv
}
