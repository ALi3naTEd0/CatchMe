package download

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ALi3naTEd0/CatchMe/catchmed/internal/events"
)

type managerEnv struct {
	m        *Manager
	dir      string
	stateDir string
	cancel   context.CancelFunc
	stopped  chan struct{}
}

func newManagerEnv(t *testing.T, dir, stateDir string, maxActive int) *managerEnv {
	t.Helper()
	m, err := NewManager(Config{
		Dir: dir, StateDir: stateDir, MaxActive: maxActive,
		Fetcher: &Fetcher{IdleTimeout: 2 * time.Second}, Options: fastOpts,
		ProgressEvery: 20 * time.Millisecond,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	env := &managerEnv{m: m, dir: dir, stateDir: stateDir, cancel: cancel, stopped: make(chan struct{})}
	go func() {
		_ = m.Run(ctx)
		close(env.stopped)
	}()
	t.Cleanup(env.stop)
	return env
}

// stop shuts the manager down and waits until every download has stopped.
func (e *managerEnv) stop() {
	e.cancel()
	<-e.stopped
}

// waitFor reads events until match returns true, failing after 10 s.
func waitFor(t *testing.T, sub *events.Subscription[Event], what string, match func(Event) bool) Event {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case ev, open := <-sub.C():
			if !open {
				t.Fatalf("waiting for %s: subscription closed (lagged=%v)", what, sub.Lagged())
			}
			if match(ev) {
				return ev
			}
		case <-timeout:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func statusIs(id string, s Status) func(Event) bool {
	return func(ev Event) bool { return ev.Download.ID == id && ev.Kind == EventChanged && ev.Download.Status == s }
}

func TestManagerDownloadsAll(t *testing.T) {
	content := testContent(300_000)
	srv := newFaultServer(t, content, `"v1"`, ok)
	env := newManagerEnv(t, t.TempDir(), t.TempDir(), 2)
	_, sub := env.m.Subscribe(1000)

	var ids []string
	for _, name := range []string{"a.bin", "b.bin", "c.bin"} {
		id, err := env.m.Add(srv.URL + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}

	running, maxRunning, completed := map[string]bool{}, 0, 0
	for completed < 3 {
		ev := waitFor(t, sub, "3 completed downloads", func(Event) bool { return true })
		if ev.Kind != EventChanged {
			continue
		}
		switch ev.Download.Status {
		case StatusRunning:
			running[ev.Download.ID] = true
			maxRunning = max(maxRunning, len(running))
		case StatusCompleted:
			delete(running, ev.Download.ID)
			completed++
			res := Result{Path: ev.Download.Path, Size: ev.Download.Total, SHA256: ev.Download.SHA256}
			checkResult(t, res, content)
		}
	}
	if maxRunning > 2 {
		t.Errorf("%d downloads ran at once, want at most MaxActive=2", maxRunning)
	}
	list := env.m.List()
	for i, s := range list {
		if s.ID != ids[i] || s.Status != StatusCompleted {
			t.Errorf("List()[%d] = %s %s, want %s completed (in the order added)", i, s.ID, s.Status, ids[i])
		}
	}
}

func TestManagerPauseResume(t *testing.T) {
	content := testContent(1_000_000)
	srv := newFaultServer(t, content, `"v1"`, func(int64, *http.Request) fault { return fault{delay: 5 * time.Millisecond} })
	env := newManagerEnv(t, t.TempDir(), t.TempDir(), 3)
	_, sub := env.m.Subscribe(1000)

	id, _ := env.m.Add(srv.URL + "/f.bin")
	waitFor(t, sub, "progress", func(ev Event) bool { return ev.Kind == EventProgress && ev.Download.Done > 0 })
	if err := env.m.Pause(id); err != nil {
		t.Fatal(err)
	}
	paused := waitFor(t, sub, "paused", statusIs(id, StatusPaused))
	if paused.Download.Done == 0 || paused.Download.Done >= paused.Download.Total {
		t.Fatalf("paused at %d of %d", paused.Download.Done, paused.Download.Total)
	}
	if paused.Download.Speed != 0 {
		t.Error("a paused download still reports speed")
	}
	sentAtPause := srv.bytesSent.Load()

	if err := env.m.Resume(id); err != nil {
		t.Fatal(err)
	}
	done := waitFor(t, sub, "completed", statusIs(id, StatusCompleted))
	checkResult(t, Result{Path: done.Download.Path, Size: done.Download.Total, SHA256: done.Download.SHA256}, content)
	// In-flight bytes at the moment of the pause may be sent twice; a restart would send everything again.
	if resent := srv.bytesSent.Load() - sentAtPause; resent > int64(len(content))-paused.Download.Done+64<<10 {
		t.Errorf("resume downloaded %d bytes; only %d were missing", resent, int64(len(content))-paused.Download.Done)
	}
}

func TestManagerCancelDeletesPartialData(t *testing.T) {
	content := testContent(1_000_000)
	srv := newFaultServer(t, content, `"v1"`, func(int64, *http.Request) fault { return fault{delay: 5 * time.Millisecond} })
	env := newManagerEnv(t, t.TempDir(), t.TempDir(), 3)
	_, sub := env.m.Subscribe(1000)

	id, _ := env.m.Add(srv.URL + "/f.bin")
	waitFor(t, sub, "progress", func(ev Event) bool { return ev.Kind == EventProgress && ev.Download.Done > 0 })
	if err := env.m.Cancel(id); err != nil {
		t.Fatal(err)
	}
	waitFor(t, sub, "removed", func(ev Event) bool { return ev.Kind == EventRemoved && ev.Download.ID == id })
	for _, d := range []string{env.dir, env.stateDir} {
		if entries, _ := os.ReadDir(d); len(entries) != 0 {
			t.Errorf("%s not empty after cancel: %v", d, entries)
		}
	}
	if len(env.m.List()) != 0 {
		t.Error("canceled download still listed")
	}
}

func TestManagerSurvivesRestart(t *testing.T) {
	content := testContent(1_000_000)
	var throttled atomic.Bool
	throttled.Store(true)
	srv := newFaultServer(t, content, `"v1"`, func(int64, *http.Request) fault {
		if throttled.Load() {
			return fault{delay: 5 * time.Millisecond}
		}
		return fault{}
	})
	dir, stateDir := t.TempDir(), t.TempDir()

	first := newManagerEnv(t, dir, stateDir, 3)
	_, sub := first.m.Subscribe(1000)
	id, _ := first.m.Add(srv.URL + "/f.bin")
	waitFor(t, sub, "progress", func(ev Event) bool { return ev.Kind == EventProgress && ev.Download.Done > 100_000 })
	first.stop() // the program closes in the middle of the download

	throttled.Store(false)
	sentBefore := srv.bytesSent.Load()
	second := newManagerEnv(t, dir, stateDir, 3)
	list, sub2 := second.m.Subscribe(1000)
	if len(list) != 1 || list[0].ID != id || list[0].Done == 0 {
		t.Fatalf("after restart the list is %+v, want the download with its progress", list)
	}
	if list[0].Status != StatusQueued && list[0].Status != StatusRunning {
		t.Fatalf("status after restart = %s, want it to continue by itself", list[0].Status)
	}
	missing := int64(len(content)) - list[0].Done

	done := waitFor(t, sub2, "completed", statusIs(id, StatusCompleted))
	checkResult(t, Result{Path: done.Download.Path, Size: done.Download.Total, SHA256: done.Download.SHA256}, content)
	if sent := srv.bytesSent.Load() - sentBefore; sent != missing {
		t.Errorf("after restart downloaded %d bytes, want the %d missing", sent, missing)
	}
}

func TestManagerFailedThenResume(t *testing.T) {
	content := testContent(100_000)
	var broken atomic.Bool
	broken.Store(true)
	srv := newFaultServer(t, content, `"v1"`, func(int64, *http.Request) fault {
		if broken.Load() {
			return fault{status: http.StatusForbidden}
		}
		return fault{}
	})
	env := newManagerEnv(t, t.TempDir(), t.TempDir(), 3)
	_, sub := env.m.Subscribe(1000)

	id, _ := env.m.Add(srv.URL + "/f.bin")
	failed := waitFor(t, sub, "failed", statusIs(id, StatusFailed))
	if failed.Download.Error == "" {
		t.Error("failed download has no error message")
	}

	broken.Store(false)
	if err := env.m.Resume(id); err != nil {
		t.Fatal(err)
	}
	done := waitFor(t, sub, "completed", statusIs(id, StatusCompleted))
	if done.Download.Error != "" {
		t.Errorf("error message kept after success: %q", done.Download.Error)
	}
	checkResult(t, Result{Path: done.Download.Path, Size: done.Download.Total, SHA256: done.Download.SHA256}, content)
}

func TestManagerRemoteChangedStartsOver(t *testing.T) {
	v1, v2 := testContent(500_000), testContent(400_000)
	var current atomic.Pointer[[]byte]
	current.Store(&v1)
	var throttled atomic.Bool
	throttled.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content := *current.Load()
		w.Header().Set("Etag", fmt.Sprintf(`"len-%d"`, len(content)))
		if throttled.Load() && r.Header.Get("Range") != "bytes=0-0" {
			w = &slowWriter{ResponseWriter: w, delay: 5 * time.Millisecond, done: r.Context().Done()}
		}
		http.ServeContent(w, r, "", modTime, bytes.NewReader(content))
	}))
	t.Cleanup(srv.Close)

	env := newManagerEnv(t, t.TempDir(), t.TempDir(), 3)
	_, sub := env.m.Subscribe(1000)
	id, _ := env.m.Add(srv.URL + "/f.bin")
	waitFor(t, sub, "progress", func(ev Event) bool { return ev.Kind == EventProgress && ev.Download.Done > 0 })
	if err := env.m.Pause(id); err != nil {
		t.Fatal(err)
	}
	waitFor(t, sub, "paused", statusIs(id, StatusPaused))

	current.Store(&v2) // the file is replaced on the server while paused
	throttled.Store(false)
	if err := env.m.Resume(id); err != nil {
		t.Fatal(err)
	}
	failed := waitFor(t, sub, "failed", statusIs(id, StatusFailed))
	if !strings.Contains(failed.Download.Error, ErrRemoteChanged.Error()) {
		t.Errorf("error = %q, want it to say the file changed", failed.Download.Error)
	}

	if err := env.m.Resume(id); err != nil { // second try starts from zero
		t.Fatal(err)
	}
	done := waitFor(t, sub, "completed", statusIs(id, StatusCompleted))
	checkResult(t, Result{Path: done.Download.Path, Size: done.Download.Total, SHA256: done.Download.SHA256}, v2)
	if filepath.Base(done.Download.Path) != "f.bin" {
		t.Errorf("saved as %s; the old partial file should have been replaced, not kept beside it", done.Download.Path)
	}
}

func TestManagerRemoveKeepsTheFile(t *testing.T) {
	content := testContent(50_000)
	srv := rangeServer(t, content, "", "")
	env := newManagerEnv(t, t.TempDir(), t.TempDir(), 3)
	_, sub := env.m.Subscribe(100)
	id, _ := env.m.Add(srv.URL + "/keep.bin")
	done := waitFor(t, sub, "completed", statusIs(id, StatusCompleted))

	if err := env.m.Cancel(id); !errors.Is(err, ErrNotAllowed) {
		t.Errorf("Cancel(completed) err = %v, want ErrNotAllowed", err)
	}
	if err := env.m.Remove(id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(done.Download.Path); err != nil {
		t.Errorf("Remove deleted the downloaded file: %v", err)
	}
	if entries, _ := os.ReadDir(env.stateDir); len(entries) != 0 {
		t.Errorf("records left after Remove: %v", entries)
	}
}

func TestManagerRejects(t *testing.T) {
	env := newManagerEnv(t, t.TempDir(), t.TempDir(), 1)
	if _, err := env.m.Add("ftp://x/y"); !errors.Is(err, ErrUnsupportedURL) {
		t.Errorf("Add(ftp) err = %v", err)
	}
	for name, fn := range map[string]func(string) error{"Pause": env.m.Pause, "Resume": env.m.Resume, "Cancel": env.m.Cancel, "Remove": env.m.Remove} {
		if err := fn("nope"); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s(unknown) err = %v, want ErrNotFound", name, err)
		}
	}
}
