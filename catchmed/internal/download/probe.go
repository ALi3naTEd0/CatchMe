package download

import (
	"context"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Info is what the server tells us about a file before we download it.
type Info struct {
	URL         string // final URL after redirects; download from here
	Size        int64  // bytes, or -1 when the server does not say
	Ranges      bool   // the file can be downloaded in parallel parts and resumed
	Filename    string // safe to use as a file name on any OS
	Validator   string // strong ETag or Last-Modified, sent as If-Range on later requests; "" if none
	ContentType string
}

// Probe asks the server about rawURL with a one-byte range request. A GET for
// bytes=0-0 answers everything at once (size, range support, name) and works
// on servers that handle HEAD badly. The body is never read.
func (f *Fetcher) Probe(ctx context.Context, rawURL string) (Info, error) {
	ctx, _, stop := f.watchdog(ctx)
	defer stop()

	resp, err := f.get(ctx, rawURL, http.Header{"Range": {"bytes=0-0"}})
	if err != nil {
		return Info{}, cause(ctx, err)
	}
	defer resp.Body.Close()

	info := Info{
		URL:         resp.Request.URL.String(),
		Size:        -1,
		ContentType: resp.Header.Get("Content-Type"),
		Filename:    filenameFrom(resp.Header.Get("Content-Disposition"), resp.Request.URL),
	}

	switch resp.StatusCode {
	case http.StatusPartialContent:
		_, _, size, ok := parseContentRange(resp.Header.Get("Content-Range"))
		if !ok {
			return Info{}, fmt.Errorf("download: invalid Content-Range %q", resp.Header.Get("Content-Range"))
		}
		info.Size = size
		info.Ranges = size >= 0 // without a total size there is nothing to split
	case http.StatusOK:
		info.Size = resp.ContentLength // already -1 when unknown
	case http.StatusRequestedRangeNotSatisfiable:
		// The only valid reason to reject bytes=0-0 is an empty file: "bytes */0".
		if _, _, size, ok := parseContentRange(resp.Header.Get("Content-Range")); ok && size == 0 {
			info.Size = 0
		} else {
			return Info{}, statusError(resp)
		}
	default:
		return Info{}, statusError(resp)
	}

	if info.Ranges {
		info.Validator = validator(resp.Header)
	}
	return info, nil
}

// validator picks what to send as If-Range. RFC 9110 only allows a strong
// ETag or a date there, so weak ETags (W/"...") fall back to Last-Modified.
func validator(h http.Header) string {
	if etag := h.Get("Etag"); etag != "" && !strings.HasPrefix(etag, "W/") {
		return etag
	}
	return h.Get("Last-Modified")
}

// parseContentRange reads "bytes 0-99/1000", "bytes 0-99/*" or "bytes */1000".
// size is -1 when the total is "*"; start and end are -1 for "*/size".
func parseContentRange(s string) (start, end, size int64, ok bool) {
	rest, found := strings.CutPrefix(s, "bytes ")
	if !found {
		return 0, 0, 0, false
	}
	rng, total, found := strings.Cut(rest, "/")
	if !found {
		return 0, 0, 0, false
	}

	size = -1
	if total != "*" {
		n, err := strconv.ParseInt(total, 10, 64)
		if err != nil || n < 0 {
			return 0, 0, 0, false
		}
		size = n
	}
	if rng == "*" {
		return -1, -1, size, size >= 0
	}

	a, b, found := strings.Cut(rng, "-")
	if !found {
		return 0, 0, 0, false
	}
	start, err1 := strconv.ParseInt(a, 10, 64)
	end, err2 := strconv.ParseInt(b, 10, 64)
	if err1 != nil || err2 != nil || start < 0 || end < start || (size >= 0 && end >= size) {
		return 0, 0, 0, false
	}
	return start, end, size, true
}

// filenameFrom chooses the file name: Content-Disposition first (including the
// UTF-8 filename*= form), then the last segment of the URL path without the
// query string, then "download".
func filenameFrom(disposition string, u *url.URL) string {
	if disposition != "" {
		// ParseMediaType also decodes filename*=UTF-8''... into params["filename"].
		if _, params, err := mime.ParseMediaType(disposition); err == nil {
			if name := sanitizeFilename(params["filename"]); name != "" {
				return name
			}
		}
	}
	if u != nil {
		if name := sanitizeFilename(path.Base(u.Path)); name != "" {
			return name
		}
	}
	return "download"
}

const maxFilenameBytes = 200 // under the 255-byte limit of common filesystems, with room for ".part"

// sanitizeFilename turns a name from the network into one that is safe to
// create on Linux, macOS and Windows: no directories (so no "../" escapes),
// no characters Windows rejects, no reserved device names, bounded length.
// It returns "" when nothing usable is left.
func sanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	name = path.Base(name)
	if name == "/" || name == "." || name == ".." {
		return "" // path.Base of an empty or directory-only path
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, " .")
	if name == "" {
		return ""
	}

	base, ext := splitExt(name)
	if isReservedWindowsName(base) {
		base = "_" + base
	}
	if len(base)+len(ext) > maxFilenameBytes {
		base = truncateUTF8(base, maxFilenameBytes-len(ext))
	}
	return base + ext
}

// splitExt splits "movie.2024.mkv" into "movie.2024" and ".mkv". Long or
// missing extensions stay in the base so truncation cannot eat the whole name.
func splitExt(name string) (base, ext string) {
	ext = path.Ext(name)
	if ext == name || len(ext) > 16 {
		return name, ""
	}
	return strings.TrimSuffix(name, ext), ext
}

func isReservedWindowsName(base string) bool {
	switch strings.ToUpper(base) {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return true
	}
	return false
}

// truncateUTF8 cuts s to at most n bytes without splitting a character.
func truncateUTF8(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
