package download

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fastOpts keeps tests quick: small ranges so a 1 MB file still uses several
// connections, and retry waits of milliseconds instead of seconds.
var fastOpts = Options{MinPart: 64 << 10, MaxParts: 4, MaxRetries: 3, RetryBase: time.Millisecond, RetryMax: 5 * time.Millisecond}

func newTask(srvURL, dir string) *Task {
	return &Task{URL: srvURL, Dir: dir, Fetcher: &Fetcher{IdleTimeout: 2 * time.Second}, Options: fastOpts}
}

// checkResult verifies the file on disk, the reported hash and that no .part is left.
func checkResult(t *testing.T, res Result, want []byte) {
	t.Helper()
	got, err := os.ReadFile(res.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s: content differs (%d bytes, want %d)", res.Path, len(got), len(want))
	}
	sum := sha256.Sum256(want)
	if res.SHA256 != hex.EncodeToString(sum[:]) || res.Size != int64(len(want)) {
		t.Errorf("Result = %+v, want size %d and the content's SHA-256", res, len(want))
	}
	if _, err := os.Stat(res.Path + ".part"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf(".part file left behind")
	}
}

func TestTaskParallel(t *testing.T) {
	content := testContent(1_000_003)
	srv := newFaultServer(t, content, `"v1"`, ok)
	dir := t.TempDir()

	task := newTask(srv.URL+"/files/big.iso?token=1", dir)
	res, err := task.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	checkResult(t, res, content)
	if res.Path != filepath.Join(dir, "big.iso") {
		t.Errorf("Path = %s, want big.iso in the target dir", res.Path)
	}
	if done, total := task.Progress(); done != int64(len(content)) || total != int64(len(content)) {
		t.Errorf("Progress() = %d/%d, want %d/%d", done, total, len(content), len(content))
	}
	if srv.requests.Load() < 2 {
		t.Errorf("only %d range requests: the file was not split", srv.requests.Load())
	}
}

func TestTaskRespectsMaxParts(t *testing.T) {
	content := testContent(2_000_000)
	srv := newFaultServer(t, content, "", ok)
	task := newTask(srv.URL+"/f.bin", t.TempDir())
	task.Options.MaxParts = 3
	task.Options.MinPart = 16 << 10
	if _, err := task.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m := srv.maxInFlight.Load(); m > 3 {
		t.Errorf("%d connections at once, want at most 3", m)
	}
}

func TestTaskSequentialFallbacks(t *testing.T) {
	content := testContent(300_000)
	tests := map[string]string{
		"no ranges":    noRangeServer(t, content).URL + "/a.bin",
		"unknown size": unknownSizeServer(t, content).URL + "/a.bin",
	}
	for name, u := range tests {
		t.Run(name, func(t *testing.T) {
			res, err := newTask(u, t.TempDir()).Run(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			checkResult(t, res, content)
		})
	}
}

func TestTaskServerLiesAboutRanges(t *testing.T) {
	// The probe gets a proper 206, but every range request gets the whole file.
	content := testContent(500_000)
	srv := newFaultServer(t, content, "", func(n int64, r *http.Request) fault {
		if r.Header.Get("Range") != "" {
			return fault{ignoreRange: true}
		}
		return fault{}
	})
	res, err := newTask(srv.URL+"/x.bin", t.TempDir()).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	checkResult(t, res, content)
}

func TestTaskEmptyFile(t *testing.T) {
	srv := rangeServer(t, nil, `"e"`, "")
	res, err := newTask(srv.URL+"/empty.txt", t.TempDir()).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	checkResult(t, res, nil)
}

func TestTaskNameCollisions(t *testing.T) {
	content := testContent(1000)
	srv := rangeServer(t, content, "", "")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "song.flac"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"song (1).flac", "song (2).flac"} {
		res, err := newTask(srv.URL+"/song.flac", dir).Run(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Base(res.Path) != want {
			t.Errorf("download %d saved as %q, want %q", i+1, filepath.Base(res.Path), want)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "song.flac")); string(b) != "mine" {
		t.Error("the existing file was overwritten")
	}
}

func TestTaskRetriesTransientErrors(t *testing.T) {
	content := testContent(1_000_000)
	srv := newFaultServer(t, content, `"v1"`, func(n int64, r *http.Request) fault {
		switch n {
		case 1, 2:
			return fault{status: http.StatusServiceUnavailable}
		case 3:
			return fault{cutAfter: 10_000}
		}
		return fault{}
	})
	res, err := newTask(srv.URL+"/f.bin", t.TempDir()).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	checkResult(t, res, content)
}

// Every response is cut after 20 KB, more times than MaxRetries allows.
// Because each attempt makes progress and resumes where it stopped, the
// download still completes.
func TestTaskKeepsGoingWhileMakingProgress(t *testing.T) {
	content := testContent(400_000)
	srv := newFaultServer(t, content, `"v1"`, func(int64, *http.Request) fault { return fault{cutAfter: 20_000} })
	task := newTask(srv.URL+"/f.bin", t.TempDir())
	task.Options.MaxParts = 1
	task.Options.MinPart = 1 << 30 // one range, so it needs ~20 resumes
	res, err := task.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	checkResult(t, res, content)
	if n := srv.requests.Load(); n < 20 {
		t.Errorf("%d requests; expected about 20 resumes", n)
	}
}

func TestTaskGivesUp(t *testing.T) {
	srv := newFaultServer(t, testContent(100_000), "", func(int64, *http.Request) fault {
		return fault{status: http.StatusBadGateway}
	})
	task := newTask(srv.URL+"/f.bin", t.TempDir())
	task.Options.MaxParts = 1
	task.Options.MinPart = 1 << 30
	_, err := task.Run(context.Background())
	var se *StatusError
	if !errors.As(err, &se) || se.Code != http.StatusBadGateway {
		t.Fatalf("err = %v, want a 502 StatusError", err)
	}
	if n := srv.requests.Load(); n != int64(fastOpts.MaxRetries)+1 {
		t.Errorf("%d requests, want %d (1 + MaxRetries)", n, fastOpts.MaxRetries+1)
	}
}

func TestTaskDoesNotRetryPermanentErrors(t *testing.T) {
	srv := newFaultServer(t, testContent(100_000), "", func(int64, *http.Request) fault {
		return fault{status: http.StatusForbidden}
	})
	task := newTask(srv.URL+"/f.bin", t.TempDir())
	task.Options.MaxParts = 1
	task.Options.MinPart = 1 << 30
	_, err := task.Run(context.Background())
	var se *StatusError
	if !errors.As(err, &se) || se.Code != http.StatusForbidden {
		t.Fatalf("err = %v, want a 403 StatusError", err)
	}
	if n := srv.requests.Load(); n != 1 {
		t.Errorf("%d requests; a 403 must not be retried", n)
	}
}

func TestTaskProbeNotFound(t *testing.T) {
	srv := statusServer(t, http.StatusNotFound)
	dir := t.TempDir()
	_, err := newTask(srv.URL+"/missing.zip", dir).Run(context.Background())
	var se *StatusError
	if !errors.As(err, &se) || se.Code != http.StatusNotFound {
		t.Fatalf("err = %v, want 404", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("files created for a download that never started: %v", entries)
	}
}

func TestTaskRemoteChanged(t *testing.T) {
	content := testContent(500_000)
	srv := newFaultServer(t, content, `"v1"`, func(n int64, r *http.Request) fault {
		return fault{etag: `"v2"`} // the file was replaced right after the probe
	})
	_, err := newTask(srv.URL+"/f.bin", t.TempDir()).Run(context.Background())
	if !errors.Is(err, ErrRemoteChanged) {
		t.Fatalf("err = %v, want ErrRemoteChanged", err)
	}
}

func TestTaskHonorsRetryAfter(t *testing.T) {
	content := testContent(10_000)
	srv := newFaultServer(t, content, "", func(n int64, r *http.Request) fault {
		if n == 1 {
			return fault{status: http.StatusTooManyRequests, retryAfter: "1"}
		}
		return fault{}
	})
	start := time.Now()
	res, err := newTask(srv.URL+"/f.bin", t.TempDir()).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	checkResult(t, res, content)
	if d := time.Since(start); d < time.Second {
		t.Errorf("finished in %v; the server asked to wait 1 s", d)
	}
}

func TestTaskCancel(t *testing.T) {
	content := testContent(2_000_000)
	srv := slowServer(t, content, 1000, 20*time.Millisecond) // would take ~40 s
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(200*time.Millisecond, cancel)

	task := newTask(srv.URL+"/slow.bin", dir)
	task.Options.MaxParts = 1 // slowServer only knows how to send the full range
	task.Options.MinPart = 1 << 30
	start := time.Now()
	_, err := task.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("took %v to stop after cancel", d)
	}
	if _, err := os.Stat(filepath.Join(dir, "slow.bin")); !errors.Is(err, os.ErrNotExist) {
		t.Error("a canceled download produced a final file")
	}
}

func TestTaskUnsupportedURL(t *testing.T) {
	for _, u := range []string{"ftp://example.com/f", "file:///etc/passwd", "not a url", "http://"} {
		if _, err := newTask(u, t.TempDir()).Run(context.Background()); !errors.Is(err, ErrUnsupportedURL) {
			t.Errorf("%q: err = %v, want ErrUnsupportedURL", u, err)
		}
	}
}

func TestRetryAfterHeader(t *testing.T) {
	now := time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)
	tests := map[string]time.Duration{
		"":                              0,
		"120":                           2 * time.Minute,
		"0":                             0,
		"soon":                          0,
		"Wed, 25 Nov 2026 12:00:30 GMT": 30 * time.Second,
		"Wed, 25 Nov 2026 11:00:00 GMT": 0, // in the past
	}
	for in, want := range tests {
		if got := retryAfter(in, now); got != want {
			t.Errorf("retryAfter(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestBackoffBounds(t *testing.T) {
	opts := Options{RetryBase: 100 * time.Millisecond, RetryMax: time.Second}.withDefaults()
	for failures := 1; failures <= 30; failures++ {
		for range 50 {
			d := backoff(opts, failures, errors.New("x"))
			full := min(opts.RetryBase<<min(failures-1, 20), opts.RetryMax)
			if d < full/2 || d > full {
				t.Fatalf("failure %d: wait %v outside [%v, %v]", failures, d, full/2, full)
			}
		}
	}
}
