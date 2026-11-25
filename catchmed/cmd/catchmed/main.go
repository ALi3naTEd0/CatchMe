// Command catchmed is the CatchMe download daemon. The app starts it and
// talks to it over a WebSocket on 127.0.0.1; it can also run on its own.
//
// When it is ready it prints one JSON line to stdout:
//
//	{"ready":true,"addr":"127.0.0.1:47800","protocol":1,"version":"..."}
//
// The token comes from the CATCHME_TOKEN environment variable; without it a
// random one is generated and saved in <state>/token (readable only by you).
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/ALi3naTEd0/CatchMe/catchmed/internal/api"
	"github.com/ALi3naTEd0/CatchMe/catchmed/internal/download"
)

// version is set at build time: -ldflags "-X main.version=1.2.3".
var version = "dev"

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "catchmed:", err)
		os.Exit(1)
	}
}

type config struct {
	addr           string
	dir            string
	stateDir       string
	maxActive      int
	exitWithParent bool
}

func parseFlags(args []string, stderr io.Writer) (config, error) {
	home, _ := os.UserHomeDir()
	confDir, _ := os.UserConfigDir()
	var c config
	fs := flag.NewFlagSet("catchmed", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&c.addr, "addr", "127.0.0.1:47800", "listen address; must be loopback (port 0 picks a free one)")
	fs.StringVar(&c.dir, "dir", filepath.Join(home, "Downloads"), "where finished files go")
	fs.StringVar(&c.stateDir, "state", filepath.Join(confDir, "catchme"), "where the download list, progress and token are kept")
	fs.IntVar(&c.maxActive, "max-active", 3, "downloads running at the same time")
	fs.BoolVar(&c.exitWithParent, "exit-with-parent", false, "stop when stdin closes (used by the app, so the daemon never outlives it)")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	return c, nil
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	cfg, err := parseFlags(args, stderr)
	if err != nil {
		return err
	}
	log := slog.New(slog.NewTextHandler(stderr, nil))

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	if cfg.exitWithParent {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()
		go func() {
			_, _ = io.Copy(io.Discard, stdin) // returns when the parent closes our stdin or dies
			cancel()
		}()
	}

	ln, err := listenLoopback(cfg.addr)
	if err != nil {
		return err
	}
	token, err := loadToken(cfg.stateDir)
	if err != nil {
		ln.Close()
		return err
	}
	mgr, err := download.NewManager(download.Config{Dir: cfg.dir, StateDir: filepath.Join(cfg.stateDir, "downloads"), MaxActive: cfg.maxActive})
	if err != nil {
		ln.Close()
		return err
	}

	srv := &http.Server{
		Handler:           (&api.Server{Manager: mgr, Token: token, Version: version, Logger: log}).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	mgrDone := make(chan error, 1)
	go func() { mgrDone <- mgr.Run(ctx) }()
	srvDone := make(chan error, 1)
	go func() { srvDone <- srv.Serve(ln) }()

	ready, _ := json.Marshal(map[string]any{"ready": true, "addr": ln.Addr().String(), "protocol": api.ProtocolVersion, "version": version})
	fmt.Fprintln(stdout, string(ready))
	log.Info("catchmed ready", "addr", ln.Addr().String(), "dir", cfg.dir, "version", version)

	select {
	case <-ctx.Done():
	case err := <-srvDone:
		return fmt.Errorf("server stopped: %w", err)
	}

	log.Info("shutting down: pausing downloads so they continue next time")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx) // WebSockets are hijacked; the manager closing their bus ends them
	if err := <-mgrDone; err != nil {
		return err
	}
	return nil
}

// listenLoopback refuses anything that is not 127.0.0.1, ::1 or localhost:
// the API has no TLS and is meant only for this machine.
func listenLoopback(addr string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("bad -addr %q: %w", addr, err)
	}
	if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return nil, fmt.Errorf("-addr %q is not a loopback address; catchmed only listens on this machine", addr)
	}
	return net.Listen("tcp", addr)
}

// loadToken returns CATCHME_TOKEN, or the token saved in stateDir, creating
// it the first time with permissions only the user can read.
func loadToken(stateDir string) (string, error) {
	if t := strings.TrimSpace(os.Getenv("CATCHME_TOKEN")); t != "" {
		return t, nil
	}
	path := filepath.Join(stateDir, "token")
	if b, err := os.ReadFile(path); err == nil {
		if t := strings.TrimSpace(string(b)); t != "" {
			return t, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return "", err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	t := hex.EncodeToString(b)
	if err := os.WriteFile(path, []byte(t+"\n"), 0o600); err != nil {
		return "", err
	}
	return t, nil
}
