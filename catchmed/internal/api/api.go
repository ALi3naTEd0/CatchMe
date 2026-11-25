// Package api exposes the download manager to the CatchMe app over a local
// WebSocket. It only answers on loopback, to requests that carry the token,
// so web pages and other machines cannot drive it.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/ALi3naTEd0/CatchMe/catchmed/internal/download"
)

// ProtocolVersion changes when messages change in a way old apps cannot read.
const ProtocolVersion = 1

// CloseLagged is the WebSocket close code sent to a client that fell too far
// behind on events. It should reconnect: the new hello has the full list.
const CloseLagged websocket.StatusCode = 4000

// Server serves the API. Token is required; AllowedOrigins lists browser
// origins (scheme://host[:port]) that may connect, normally none.
type Server struct {
	Manager        *download.Manager
	Token          string
	Version        string
	AllowedOrigins []string
	Logger         *slog.Logger
}

// Handler returns the HTTP handler:
//
//	GET /healthz      no token; for the app to know the daemon is up
//	GET /v1/downloads the list as JSON
//	GET /v1/ws        WebSocket: hello, then events and command replies
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.Handle("GET /v1/downloads", s.guard(http.HandlerFunc(s.list)))
	mux.Handle("GET /v1/ws", s.guard(http.HandlerFunc(s.ws)))
	return loopbackOnly(mux)
}

func (s *Server) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

// loopbackOnly rejects requests whose Host is not a loopback name. This
// stops DNS rebinding: a web page cannot reach us through a domain it
// controls that resolves to 127.0.0.1.
func loopbackOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(r.Host); err == nil {
			host = h
		}
		host = strings.Trim(host, "[]")
		if host != "localhost" && host != "127.0.0.1" && host != "::1" {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// guard checks the Origin and the token. Browsers always send Origin, so
// rejecting unknown ones blocks web pages even before the token check;
// the desktop app sends none.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if o := r.Header.Get("Origin"); o != "" && !slices.Contains(s.AllowedOrigins, o) {
			http.Error(w, "forbidden origin", http.StatusForbidden)
			return
		}
		got, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if s.Token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.Token)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "missing or wrong token", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{"ok": true, "version": s.Version, "protocol": ProtocolVersion})
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.Manager.List())
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// Messages from the server.

// Hello is the first message on every connection: the whole list.
type Hello struct {
	Type      string              `json:"type"` // "hello"
	Protocol  int                 `json:"protocol"`
	Version   string              `json:"version"`
	Downloads []download.Snapshot `json:"downloads"`
}

// EventMessage carries one manager event.
type EventMessage struct {
	Type  string         `json:"type"` // "event"
	Event download.Event `json:"event"`
}

// Reply answers one Command, matched by ID.
type Reply struct {
	Type       string     `json:"type"` // "reply"
	ID         string     `json:"id"`
	OK         bool       `json:"ok"`
	DownloadID string     `json:"downloadId,omitempty"`
	Error      *ErrorBody `json:"error,omitempty"`
}

// ErrorBody is a machine-readable code plus a message for people.
type ErrorBody struct {
	Code    string `json:"code"` // bad_request, bad_url, not_found, not_allowed, internal
	Message string `json:"message"`
}

// Command is a message from the app. ID is chosen by the app and echoed in the Reply.
type Command struct {
	Type       string `json:"type"` // add, pause, resume, cancel, remove
	ID         string `json:"id"`
	URL        string `json:"url,omitempty"`        // add
	DownloadID string `json:"downloadId,omitempty"` // the others
}

const writeTimeout = 10 * time.Second

func (s *Server) ws(w http.ResponseWriter, r *http.Request) {
	// Origin was already checked by guard; an allowed origin may differ from Host.
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return // Accept already wrote the HTTP error
	}
	defer c.CloseNow()
	c.SetReadLimit(64 << 10)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	list, sub := s.Manager.Subscribe(512)
	defer sub.Close()

	replies := make(chan Reply, 16)
	go func() { // reader: commands in, replies out
		defer cancel()
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return // closed by the app, or the connection dropped
			}
			// Unmarshal here instead of wsjson.Read, which closes the connection
			// on bad JSON: one broken message should not cost the app its events.
			var rep Reply
			var cmd Command
			if err := json.Unmarshal(data, &cmd); err != nil {
				rep = Reply{Type: "reply", Error: &ErrorBody{"bad_request", "invalid JSON: " + err.Error()}}
			} else {
				rep = s.execute(cmd)
			}
			select {
			case replies <- rep:
			case <-ctx.Done():
				return
			}
		}
	}()

	if err := write(ctx, c, Hello{Type: "hello", Protocol: ProtocolVersion, Version: s.Version, Downloads: list}); err != nil {
		return
	}
	for { // writer: the only goroutine that writes to c
		select {
		case ev, open := <-sub.C():
			if !open {
				if sub.Lagged() {
					c.Close(CloseLagged, "too far behind; reconnect for the full list")
				} else {
					c.Close(websocket.StatusGoingAway, "shutting down")
				}
				return
			}
			if err := write(ctx, c, EventMessage{Type: "event", Event: ev}); err != nil {
				return
			}
		case rep := <-replies:
			if err := write(ctx, c, rep); err != nil {
				return
			}
		case <-ctx.Done():
			c.Close(websocket.StatusNormalClosure, "")
			return
		}
	}
}

func write(ctx context.Context, c *websocket.Conn, v any) error {
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	return wsjson.Write(ctx, c, v)
}

// execute runs one command against the manager.
func (s *Server) execute(cmd Command) Reply {
	rep := Reply{Type: "reply", ID: cmd.ID}
	var err error
	switch cmd.Type {
	case "add":
		rep.DownloadID, err = s.Manager.Add(cmd.URL)
	case "pause":
		err = s.Manager.Pause(cmd.DownloadID)
	case "resume":
		err = s.Manager.Resume(cmd.DownloadID)
	case "cancel":
		err = s.Manager.Cancel(cmd.DownloadID)
	case "remove":
		err = s.Manager.Remove(cmd.DownloadID)
	default:
		err = errUnknownCommand
	}
	if err != nil {
		rep.Error = errorBody(err)
		if rep.Error.Code == "internal" {
			s.logger().Error("command failed", "type", cmd.Type, "err", err)
		}
		return rep
	}
	rep.OK = true
	return rep
}

var errUnknownCommand = errors.New("unknown command type")

func errorBody(err error) *ErrorBody {
	code := "internal"
	switch {
	case errors.Is(err, errUnknownCommand):
		code = "bad_request"
	case errors.Is(err, download.ErrUnsupportedURL):
		code = "bad_url"
	case errors.Is(err, download.ErrNotFound):
		code = "not_found"
	case errors.Is(err, download.ErrNotAllowed):
		code = "not_allowed"
	}
	return &ErrorBody{Code: code, Message: err.Error()}
}
