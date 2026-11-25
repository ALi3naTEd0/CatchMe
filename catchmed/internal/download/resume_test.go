package download

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// pauseMidway starts a throttled download, cancels it after `after` and
// returns the saved state. It fails the test if the download finished or
// had not started.
func pauseMidway(t *testing.T, srvURL, dir, statePath string, after time.Duration) State {
	t.Helper()
	task := newTask(srvURL, dir)
	task.StatePath = statePath
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(after, cancel)
	_, err := task.Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("paused run: err = %v, want context.Canceled", err)
	}
	st, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("state after pause: %v", err)
	}
	if st.Done() == 0 || st.Done() >= st.Size {
		t.Fatalf("paused with %d of %d bytes; the test needs a download in the middle", st.Done(), st.Size)
	}
	if done, _ := task.Progress(); done != st.Done() {
		t.Errorf("state says %d bytes, Progress says %d", st.Done(), done)
	}
	return st
}

func TestPauseAndResume(t *testing.T) {
	content := testContent(1_000_000)
	var throttled atomic.Bool // read by server goroutines, set by the test
	throttled.Store(true)
	srv := newFaultServer(t, content, `"v1"`, func(int64, *http.Request) fault {
		if throttled.Load() {
			return fault{delay: 5 * time.Millisecond} // ~0.8 MB/s per connection
		}
		return fault{}
	})
	dir := t.TempDir()
	statePath := filepath.Join(dir, "f.bin.catchme.json")

	st := pauseMidway(t, srv.URL+"/f.bin", dir, statePath, 150*time.Millisecond)
	if _, err := os.Stat(filepath.Join(dir, "f.bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a paused download produced a final file")
	}

	// Resume at full speed and check it only downloads what was missing.
	throttled.Store(false)
	sentBefore := srv.bytesSent.Load()
	task := Resume(st, &Fetcher{IdleTimeout: 2 * time.Second}, fastOpts, statePath)
	res, err := task.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	checkResult(t, res, content)
	if res.Path != st.Path {
		t.Errorf("resumed into %s, want the reserved %s", res.Path, st.Path)
	}
	if sent, missing := srv.bytesSent.Load()-sentBefore, st.Size-st.Done(); sent != missing {
		t.Errorf("resume downloaded %d bytes, want exactly the %d missing", sent, missing)
	}
	if _, err := os.Stat(statePath); !errors.Is(err, os.ErrNotExist) {
		t.Error("state file left behind after success")
	}
}

func TestPauseTwiceThenFinish(t *testing.T) {
	content := testContent(1_000_000)
	srv := newFaultServer(t, content, `"v1"`, func(int64, *http.Request) fault { return fault{delay: 5 * time.Millisecond} })
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")

	first := pauseMidway(t, srv.URL+"/f.bin", dir, statePath, 100*time.Millisecond)

	task := Resume(first, &Fetcher{IdleTimeout: 2 * time.Second}, fastOpts, statePath)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	if _, err := task.Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("second pause: %v", err)
	}
	second, err := LoadState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if second.Done() <= first.Done() {
		t.Fatalf("second run made no progress: %d then %d", first.Done(), second.Done())
	}

	res, err := Resume(second, &Fetcher{IdleTimeout: 5 * time.Second}, fastOpts, statePath).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	checkResult(t, res, content)
}

// The state is saved while downloading, not only on pause: after a crash
// (here, simply never calling Run's cleanup) there is something to resume.
func TestStateSavedPeriodically(t *testing.T) {
	content := testContent(1_000_000)
	srv := newFaultServer(t, content, `"v1"`, func(int64, *http.Request) fault { return fault{delay: 5 * time.Millisecond} })
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")

	task := newTask(srv.URL+"/f.bin", dir)
	task.StatePath = statePath
	task.Options.SaveEvery = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _, _ = task.Run(ctx) }()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if st, err := LoadState(statePath); err == nil && st.Done() > 0 {
			return // saved mid-download
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no state with progress was saved while downloading")
}

func TestResumeRemoteChanged(t *testing.T) {
	content := testContent(1_000_000)
	srv := rangeServer(t, content, `"v1"`, "") // the file is now version v1
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	st := State{Version: stateVersion, URL: srv.URL + "/f.bin", Path: filepath.Join(dir, "f.bin"), Size: int64(len(content)),
		Validator: `"old"`, Parallel: true, Ranges: []RangeState{{Start: 0, End: int64(len(content)) - 1, Done: 10}}}
	if err := os.WriteFile(st.Path+".part", make([]byte, len(content)), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Resume(st, &Fetcher{}, fastOpts, statePath).Run(context.Background())
	if !errors.Is(err, ErrRemoteChanged) {
		t.Fatalf("err = %v, want ErrRemoteChanged", err)
	}
}

func TestResumeInvalidState(t *testing.T) {
	content := testContent(10_000)
	srv := rangeServer(t, content, `"v1"`, "")
	dir := t.TempDir()
	good := State{Version: stateVersion, URL: srv.URL + "/f.bin", Path: filepath.Join(dir, "f.bin"), Size: 10_000,
		Validator: `"v1"`, Parallel: true, Ranges: []RangeState{{0, 4999, 100}, {5000, 9999, 0}}}

	tests := []struct {
		name  string
		edit  func(*State)
		setup func()
	}{
		{"missing .part", func(*State) {}, func() {}},
		{"wrong version", func(s *State) { s.Version = 99 }, nil},
		{"gap between ranges", func(s *State) { s.Ranges[1].Start = 5001 }, nil},
		{"range claims too much", func(s *State) { s.Ranges[0].Done = 6000 }, nil},
		{"ranges do not reach the end", func(s *State) { s.Ranges[1].End = 9000 }, nil},
		{"final file already exists", func(*State) {}, func() { _ = os.WriteFile(good.Path, nil, 0o644) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_ = os.Remove(good.Path)
			_ = os.Remove(good.Path + ".part")
			if tt.setup == nil || tt.name == "final file already exists" {
				if err := os.WriteFile(good.Path+".part", make([]byte, 10_000), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if tt.setup != nil {
				tt.setup()
			}
			st := good
			st.Ranges = append([]RangeState(nil), good.Ranges...)
			tt.edit(&st)
			_, err := Resume(st, &Fetcher{}, fastOpts, "").Run(context.Background())
			if !errors.Is(err, ErrStateInvalid) {
				t.Fatalf("err = %v, want ErrStateInvalid", err)
			}
		})
	}
}

func TestResumeSequentialStartsOver(t *testing.T) {
	content := testContent(200_000)
	srv := noRangeServer(t, content)
	dir := t.TempDir()
	st := State{Version: stateVersion, URL: srv.URL + "/f.bin", Path: filepath.Join(dir, "f.bin"), Size: -1}
	if err := os.WriteFile(st.Path+".part", []byte("half-written garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Resume(st, &Fetcher{}, fastOpts, "").Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	checkResult(t, res, content)
}

func TestStateRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.json")
	st := State{Version: stateVersion, URL: "https://x/f", Path: "/tmp/f", Size: 100, Validator: `"e"`, Parallel: true,
		Ranges: []RangeState{{0, 49, 49}, {50, 99, 3}}}
	if err := SaveState(path, st); err != nil {
		t.Fatal(err)
	}
	got, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Done() != 52 || got.Path != st.Path || len(got.Ranges) != 2 || got.Ranges[1] != st.Ranges[1] {
		t.Errorf("round trip changed the state: %+v", got)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("temporary files left: %v", entries)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadState(path); !errors.Is(err, ErrStateInvalid) {
		t.Errorf("corrupt JSON: err = %v, want ErrStateInvalid", err)
	}
}
