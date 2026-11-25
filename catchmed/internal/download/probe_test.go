package download

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestProbeRangeServer(t *testing.T) {
	content := testContent(1000)
	srv := rangeServer(t, content, `"v1"`, `attachment; filename="report.pdf"`)

	info, err := (&Fetcher{}).Probe(context.Background(), srv.URL+"/files/x?token=abc")
	if err != nil {
		t.Fatal(err)
	}
	want := Info{URL: srv.URL + "/files/x?token=abc", Size: 1000, Ranges: true, Filename: "report.pdf", Validator: `"v1"`}
	info.ContentType = "" // ServeContent sniffs it; not what this test is about
	if info != want {
		t.Errorf("got  %+v\nwant %+v", info, want)
	}
}

func TestProbeWeakETagFallsBackToLastModified(t *testing.T) {
	srv := rangeServer(t, testContent(10), `W/"weak"`, "")
	info, err := (&Fetcher{}).Probe(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if want := modTime.Format(http.TimeFormat); info.Validator != want {
		t.Errorf("Validator = %q, want Last-Modified %q", info.Validator, want)
	}
}

func TestProbeNoRanges(t *testing.T) {
	srv := noRangeServer(t, testContent(5000))
	info, err := (&Fetcher{}).Probe(context.Background(), srv.URL+"/video.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if info.Ranges || info.Size != 5000 || info.Validator != "" || info.Filename != "video.mkv" {
		t.Errorf("got %+v, want Ranges=false Size=5000 no validator, name video.mkv", info)
	}
}

func TestProbeUnknownSize(t *testing.T) {
	srv := unknownSizeServer(t, testContent(64*1024))
	info, err := (&Fetcher{}).Probe(context.Background(), srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if info.Ranges || info.Size != -1 {
		t.Errorf("got Ranges=%v Size=%d, want false and -1", info.Ranges, info.Size)
	}
}

func TestProbeEmptyFile(t *testing.T) {
	srv := rangeServer(t, nil, `"empty"`, "")
	info, err := (&Fetcher{}).Probe(context.Background(), srv.URL+"/empty.txt")
	if err != nil {
		t.Fatal(err)
	}
	if info.Size != 0 {
		t.Errorf("Size = %d, want 0", info.Size)
	}
}

func TestProbeFollowsRedirects(t *testing.T) {
	target := rangeServer(t, testContent(10), "", "")
	redirect := httptest.NewServer(http.RedirectHandler(target.URL+"/files/movie.mkv?sig=xyz", http.StatusFound))
	t.Cleanup(redirect.Close)

	info, err := (&Fetcher{}).Probe(context.Background(), redirect.URL+"/short")
	if err != nil {
		t.Fatal(err)
	}
	if info.URL != target.URL+"/files/movie.mkv?sig=xyz" || info.Filename != "movie.mkv" {
		t.Errorf("got URL %q name %q, want the final URL and movie.mkv", info.URL, info.Filename)
	}
}

func TestProbeStatusErrors(t *testing.T) {
	tests := []struct {
		code      int
		retryable bool
	}{
		{http.StatusNotFound, false},
		{http.StatusForbidden, false},
		{http.StatusTooManyRequests, true},
		{http.StatusServiceUnavailable, true},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.code), func(t *testing.T) {
			srv := statusServer(t, tt.code)
			_, err := (&Fetcher{}).Probe(context.Background(), srv.URL)
			var se *StatusError
			if !errors.As(err, &se) {
				t.Fatalf("err = %v, want *StatusError", err)
			}
			if se.Code != tt.code || se.Retryable() != tt.retryable {
				t.Errorf("Code=%d Retryable=%v, want %d %v", se.Code, se.Retryable(), tt.code, tt.retryable)
			}
		})
	}
}

func TestProbeStall(t *testing.T) {
	srv := stallServer(t, testContent(10), -1)
	start := time.Now()
	_, err := (&Fetcher{IdleTimeout: 200 * time.Millisecond}).Probe(context.Background(), srv.URL)
	if !errors.Is(err, ErrStalled) {
		t.Fatalf("err = %v, want ErrStalled", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("took %v to notice the stall", d)
	}
}

func TestProbeSendsUserAgentAndIdentity(t *testing.T) {
	var ua, enc string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ua, enc = r.UserAgent(), r.Header.Get("Accept-Encoding")
	}))
	t.Cleanup(srv.Close)
	if _, err := (&Fetcher{}).Probe(context.Background(), srv.URL); err != nil {
		t.Fatal(err)
	}
	if ua != DefaultUserAgent || enc != "identity" {
		t.Errorf("User-Agent %q Accept-Encoding %q", ua, enc)
	}
}

func TestParseContentRange(t *testing.T) {
	tests := []struct {
		in                string
		start, end, total int64
		ok                bool
	}{
		{"bytes 0-99/1000", 0, 99, 1000, true},
		{"bytes 0-0/1", 0, 0, 1, true},
		{"bytes 500-999/*", 500, 999, -1, true},
		{"bytes */1000", -1, -1, 1000, true},
		{"bytes */0", -1, -1, 0, true},
		{"bytes */*", 0, 0, 0, false},
		{"bytes 0-1000/1000", 0, 0, 0, false}, // end past the file
		{"bytes 10-5/100", 0, 0, 0, false},    // reversed
		{"bytes -1-5/100", 0, 0, 0, false},
		{"bytes 0-99", 0, 0, 0, false},
		{"items 0-99/100", 0, 0, 0, false},
		{"", 0, 0, 0, false},
	}
	for _, tt := range tests {
		start, end, total, ok := parseContentRange(tt.in)
		if ok != tt.ok || (ok && (start != tt.start || end != tt.end || total != tt.total)) {
			t.Errorf("parseContentRange(%q) = %d, %d, %d, %v; want %d, %d, %d, %v",
				tt.in, start, end, total, ok, tt.start, tt.end, tt.total, tt.ok)
		}
	}
}

func TestFilenameFrom(t *testing.T) {
	tests := []struct {
		name, disposition, url, want string
	}{
		{"disposition", `attachment; filename="report.pdf"`, "http://x/a", "report.pdf"},
		{"UTF-8 filename*", `attachment; filename*=UTF-8''Pel%C3%ADcula%20%C3%B1.mkv`, "http://x/a", "Película ñ.mkv"},
		{"path traversal", `attachment; filename="../../etc/passwd"`, "http://x/a", "passwd"},
		{"windows traversal", `attachment; filename="..\\..\\win.ini"`, "http://x/a", "win.ini"},
		{"broken disposition falls back to URL", `attachment; filename=`, "http://x/dir/song.flac", "song.flac"},
		{"query string is dropped", "", "http://x/a/b/video.mkv?token=123&sig=abc", "video.mkv"},
		{"escaped path", "", "http://x/my%20file%231.zip", "my file#1.zip"},
		{"no path", "", "http://x/", "download"},
		{"only dots", `attachment; filename=".."`, "http://x/", "download"},
		{"characters Windows rejects", `attachment; filename="a<b>:c|d?.txt"`, "http://x/", "a_b__c_d_.txt"},
		{"reserved Windows name", `attachment; filename="con.txt"`, "http://x/", "_con.txt"},
		{"control characters", "attachment; filename=\"bad\x01name.txt\"", "http://x/", "bad_name.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(tt.url)
			if err != nil {
				t.Fatal(err)
			}
			if got := filenameFrom(tt.disposition, u); got != tt.want {
				t.Errorf("filenameFrom(%q, %q) = %q, want %q", tt.disposition, tt.url, got, tt.want)
			}
		})
	}
}

func TestSanitizeFilenameLength(t *testing.T) {
	long := strings.Repeat("ñ", 300) + ".mkv" // 2 bytes per ñ
	got := sanitizeFilename(long)
	if len(got) > maxFilenameBytes {
		t.Errorf("len = %d bytes, want at most %d", len(got), maxFilenameBytes)
	}
	if !strings.HasSuffix(got, ".mkv") {
		t.Errorf("extension lost: %q", got[len(got)-10:])
	}
	if !strings.HasPrefix(got, "ñ") || strings.ContainsRune(got, '\uFFFD') {
		t.Errorf("a character was split: %q", got)
	}
}

func FuzzSanitizeFilename(f *testing.F) {
	for _, s := range []string{"a.txt", "../../x", `C:\Windows\x.dll`, "CON", " . ", "ñ.mkv"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		got := sanitizeFilename(in)
		if got == "" {
			return
		}
		if strings.ContainsAny(got, `/\<>:"|?*`) || got == "." || got == ".." {
			t.Fatalf("unsafe name %q from %q", got, in)
		}
		if len(got) > maxFilenameBytes {
			t.Fatalf("name too long (%d bytes) from %q", len(got), in)
		}
		for _, r := range got {
			if r < 0x20 || r == 0x7f {
				t.Fatalf("control character in %q", got)
			}
		}
	})
}
