package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// startDaemon runs the daemon in-process on a free port and returns its
// address, a function that closes its stdin, and a channel with run's error.
func startDaemon(t *testing.T, extraArgs ...string) (addr string, closeStdin func(), done <-chan error) {
	t.Helper()
	state := t.TempDir()
	t.Setenv("CATCHME_TOKEN", "")
	args := append([]string{"-addr", "127.0.0.1:0", "-dir", t.TempDir(), "-state", state}, extraArgs...)

	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	errc := make(chan error, 1)
	finished := make(chan struct{}) // closed when run returns, for the cleanup
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		errc <- run(ctx, args, stdinR, stdoutW, io.Discard)
		close(finished)
	}()
	t.Cleanup(func() {
		cancel()
		stdinW.Close()
		select {
		case <-finished:
		case <-time.After(10 * time.Second):
			t.Error("daemon did not stop")
		}
	})

	line, err := bufio.NewReader(stdoutR).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	var ready struct {
		Ready bool
		Addr  string
	}
	if err := json.Unmarshal([]byte(line), &ready); err != nil || !ready.Ready || ready.Addr == "" {
		t.Fatalf("ready line %q (%v)", line, err)
	}
	go func() { _, _ = io.Copy(io.Discard, stdoutR) }()
	return ready.Addr, func() { stdinW.Close() }, errc
}

func TestDaemonStartsAndCreatesToken(t *testing.T) {
	addr, _, _ := startDaemon(t)
	resp, err := http.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz = %d", resp.StatusCode)
	}
	resp, err = http.Get("http://" + addr + "/v1/downloads")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("/v1/downloads without token = %d, want 401", resp.StatusCode)
	}
}

func TestDaemonExitsWithParent(t *testing.T) {
	_, closeStdin, done := startDaemon(t, "-exit-with-parent")
	closeStdin()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("exit error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("daemon kept running after its parent went away")
	}
}

func TestRefusesNonLoopback(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:0", ":0", "192.168.1.10:0", "example.com:80"} {
		if ln, err := listenLoopback(addr); err == nil {
			ln.Close()
			t.Errorf("listenLoopback(%q) accepted a non-loopback address", addr)
		}
	}
	for _, addr := range []string{"127.0.0.1:0", "[::1]:0", "localhost:0"} {
		ln, err := listenLoopback(addr)
		if err != nil {
			if strings.Contains(err.Error(), "loopback") {
				t.Errorf("listenLoopback(%q): %v", addr, err)
			}
			continue // e.g. no IPv6 on this machine
		}
		ln.Close()
	}
}

func TestTokenFile(t *testing.T) {
	t.Setenv("CATCHME_TOKEN", "")
	dir := t.TempDir()
	first, err := loadToken(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := loadToken(dir)
	if err != nil || second != first || len(first) != 64 {
		t.Fatalf("token not stable: %q then %q (%v)", first, second, err)
	}
	fi, err := os.Stat(filepath.Join(dir, "token"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := fi.Mode().Perm(); perm != 0o600 {
		t.Errorf("token file mode %o, want 600", perm)
	}
	t.Setenv("CATCHME_TOKEN", "from-env")
	if got, _ := loadToken(dir); got != "from-env" {
		t.Errorf("CATCHME_TOKEN ignored: got %q", got)
	}
}
