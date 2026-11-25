package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/ALi3naTEd0/CatchMe/catchmed/internal/download"
)

const token = "test-token"

// env is a manager behind the API, plus a file server to download from.
type env struct {
	api   *httptest.Server
	files *httptest.Server
	dir   string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	mgr, err := download.NewManager(download.Config{Dir: dir, StateDir: t.TempDir(), ProgressEvery: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { _ = mgr.Run(ctx); close(stopped) }()

	api := httptest.NewServer((&Server{Manager: mgr, Token: token, Version: "test"}).Handler())
	files := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader([]byte(strings.Repeat("catchme ", 10_000))))
	}))
	t.Cleanup(func() {
		cancel()
		<-stopped
		api.Close()
		files.Close()
	})
	return &env{api: api, files: files, dir: dir}
}

func (e *env) get(t *testing.T, path string, header http.Header) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, e.api.URL+path, nil)
	for k, v := range header {
		req.Header[k] = v
	}
	if h := header.Get("Host"); h != "" {
		req.Host = h
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

func bearer(tok string) http.Header { return http.Header{"Authorization": {"Bearer " + tok}} }

func TestHealthNeedsNoToken(t *testing.T) {
	e := newEnv(t)
	if resp := e.get(t, "/healthz", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz = %d", resp.StatusCode)
	}
}

func TestSecurity(t *testing.T) {
	e := newEnv(t)
	tests := []struct {
		name   string
		header http.Header
		want   int
	}{
		{"valid token", bearer(token), http.StatusOK},
		{"no token", nil, http.StatusUnauthorized},
		{"wrong token", bearer("nope"), http.StatusUnauthorized},
		{"token in the wrong scheme", http.Header{"Authorization": {"Basic " + token}}, http.StatusUnauthorized},
		{"a web page", http.Header{"Authorization": {"Bearer " + token}, "Origin": {"https://evil.example"}}, http.StatusForbidden},
		{"DNS rebinding", http.Header{"Authorization": {"Bearer " + token}, "Host": {"evil.example:47800"}}, http.StatusForbidden},
		{"localhost name is fine", http.Header{"Authorization": {"Bearer " + token}, "Host": {"localhost:47800"}}, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := e.get(t, "/v1/downloads", tt.header).StatusCode; got != tt.want {
				t.Errorf("status %d, want %d", got, tt.want)
			}
		})
	}
}

func dial(t *testing.T, e *env, tok string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	u := "ws" + strings.TrimPrefix(e.api.URL, "http") + "/v1/ws"
	return websocket.Dial(ctx, u, &websocket.DialOptions{HTTPHeader: bearer(tok)})
}

func TestWebSocketRequiresToken(t *testing.T) {
	e := newEnv(t)
	_, resp, err := dial(t, e, "wrong")
	if err == nil {
		t.Fatal("connected with a wrong token")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("response %v, want 401", resp)
	}
}

// client reads typed messages from the socket.
type client struct {
	t *testing.T
	c *websocket.Conn
}

func (cl *client) send(cmd Command) {
	cl.t.Helper()
	if err := wsjson.Write(context.Background(), cl.c, cmd); err != nil {
		cl.t.Fatal(err)
	}
}

// next returns the next message as raw JSON plus its type.
func (cl *client) next() (string, json.RawMessage) {
	cl.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var raw json.RawMessage
	if err := wsjson.Read(ctx, cl.c, &raw); err != nil {
		cl.t.Fatalf("read: %v", err)
	}
	var head struct{ Type string }
	_ = json.Unmarshal(raw, &head)
	return head.Type, raw
}

// reply waits for the reply to id, skipping events.
func (cl *client) reply(id string) Reply {
	cl.t.Helper()
	for {
		typ, raw := cl.next()
		if typ != "reply" {
			continue
		}
		var r Reply
		_ = json.Unmarshal(raw, &r)
		if r.ID == id {
			return r
		}
	}
}

func TestWebSocketFlow(t *testing.T) {
	e := newEnv(t)
	c, _, err := dial(t, e, token)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	cl := &client{t: t, c: c}

	typ, raw := cl.next()
	var hello Hello
	_ = json.Unmarshal(raw, &hello)
	if typ != "hello" || hello.Protocol != ProtocolVersion || hello.Downloads == nil {
		t.Fatalf("first message %s %s, want hello with protocol %d and a list", typ, raw, ProtocolVersion)
	}

	cl.send(Command{Type: "add", ID: "1", URL: e.files.URL + "/notes.txt"})
	rep := cl.reply("1")
	if !rep.OK || rep.DownloadID == "" {
		t.Fatalf("add reply %+v", rep)
	}

	// Events until it completes.
	for {
		typ, raw := cl.next()
		if typ != "event" {
			continue
		}
		var m EventMessage
		_ = json.Unmarshal(raw, &m)
		if m.Event.Download.ID == rep.DownloadID && m.Event.Download.Status == download.StatusCompleted {
			if b, err := os.ReadFile(m.Event.Download.Path); err != nil || len(b) != 80_000 {
				t.Fatalf("downloaded file: %d bytes, err %v", len(b), err)
			}
			break
		}
	}

	errorCases := []struct {
		cmd  Command
		code string
	}{
		{Command{Type: "pause", ID: "2", DownloadID: "missing"}, "not_found"},
		{Command{Type: "cancel", ID: "3", DownloadID: rep.DownloadID}, "not_allowed"},
		{Command{Type: "add", ID: "4", URL: "file:///etc/passwd"}, "bad_url"},
		{Command{Type: "explode", ID: "5"}, "bad_request"},
	}
	for _, tc := range errorCases {
		cl.send(tc.cmd)
		if r := cl.reply(tc.cmd.ID); r.OK || r.Error == nil || r.Error.Code != tc.code {
			t.Errorf("%s: reply %+v, want error %s", tc.cmd.Type, r, tc.code)
		}
	}

	// Broken JSON gets an error reply and the connection stays usable.
	if err := c.Write(context.Background(), websocket.MessageText, []byte("{not json")); err != nil {
		t.Fatal(err)
	}
	if r := cl.reply(""); r.Error == nil || r.Error.Code != "bad_request" {
		t.Errorf("broken JSON reply %+v", r)
	}
	cl.send(Command{Type: "remove", ID: "6", DownloadID: rep.DownloadID})
	if r := cl.reply("6"); !r.OK {
		t.Errorf("remove after bad JSON: %+v", r)
	}
}

func TestSecondClientSeesTheList(t *testing.T) {
	e := newEnv(t)
	c1, _, err := dial(t, e, token)
	if err != nil {
		t.Fatal(err)
	}
	defer c1.CloseNow()
	cl := &client{t: t, c: c1}
	cl.next() // hello
	cl.send(Command{Type: "add", ID: "1", URL: e.files.URL + "/a.txt"})
	id := cl.reply("1").DownloadID

	c2, _, err := dial(t, e, token)
	if err != nil {
		t.Fatal(err)
	}
	defer c2.CloseNow()
	_, raw := (&client{t: t, c: c2}).next()
	var hello Hello
	_ = json.Unmarshal(raw, &hello)
	if len(hello.Downloads) != 1 || hello.Downloads[0].ID != id {
		t.Errorf("second client hello has %+v, want the download added by the first", hello.Downloads)
	}
}
