package download

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// FetchRange downloads the bytes in r and writes them to w at offset r.Start,
// so several ranges can fill one preallocated file in parallel.
//
// ifRange, when not empty, is sent as If-Range (use Info.Validator): if the file
// changed on the server, FetchRange fails with ErrRemoteChanged instead of
// mixing two versions of it.
//
// It always returns how many bytes it wrote, also on error, so the caller can
// resume from r.Start+n. onProgress, if not nil, is called after every write.
func (f *Fetcher) FetchRange(ctx context.Context, rawURL string, r Range, ifRange string, w io.WriterAt, onProgress func(n int64)) (int64, error) {
	if r.Start < 0 || r.End < r.Start {
		return 0, fmt.Errorf("download: invalid range %+v", r)
	}
	ctx, kick, stop := f.watchdog(ctx)
	defer stop()

	header := http.Header{"Range": {fmt.Sprintf("bytes=%d-%d", r.Start, r.End)}}
	if ifRange != "" {
		header.Set("If-Range", ifRange)
	}
	resp, err := f.get(ctx, rawURL, header)
	if err != nil {
		return 0, cause(ctx, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusPartialContent:
	case http.StatusOK:
		// A 200 to a range request is the whole file. Either the file changed
		// (If-Range did not match) or the server does not do ranges at all. Only
		// a different validator in the response proves a change.
		if v := validator(resp.Header); ifRange != "" && v != "" && v != ifRange {
			return 0, ErrRemoteChanged
		}
		return 0, ErrRangeNotSupported
	default:
		return 0, statusError(resp)
	}

	start, end, _, ok := parseContentRange(resp.Header.Get("Content-Range"))
	if !ok || start != r.Start || end != r.End {
		return 0, fmt.Errorf("download: asked for bytes %d-%d, server sent %q", r.Start, r.End, resp.Header.Get("Content-Range"))
	}

	dst := &progressWriter{w: io.NewOffsetWriter(w, r.Start), kick: kick, onProgress: onProgress}
	n, err := io.Copy(dst, io.LimitReader(resp.Body, r.Len()))
	if err != nil {
		return n, cause(ctx, err)
	}
	if n < r.Len() {
		return n, io.ErrUnexpectedEOF
	}
	return n, nil
}

// FetchWhole downloads the file in a single sequential stream. It is the
// fallback for servers without range support or without a known size.
func (f *Fetcher) FetchWhole(ctx context.Context, rawURL string, w io.Writer, onProgress func(n int64)) (int64, error) {
	ctx, kick, stop := f.watchdog(ctx)
	defer stop()

	resp, err := f.get(ctx, rawURL, nil)
	if err != nil {
		return 0, cause(ctx, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, statusError(resp)
	}

	n, err := io.Copy(&progressWriter{w: w, kick: kick, onProgress: onProgress}, resp.Body)
	if err != nil {
		return n, cause(ctx, err)
	}
	if resp.ContentLength >= 0 && n < resp.ContentLength {
		return n, io.ErrUnexpectedEOF
	}
	return n, nil
}
