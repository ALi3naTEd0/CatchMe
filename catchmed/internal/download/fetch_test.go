package download

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// tempFile returns an empty file of the given size, the way the engine will
// preallocate the .part file before parallel ranges fill it.
func tempFile(t *testing.T, size int64) *os.File {
	t.Helper()
	f, err := os.Create(filepath.Join(t.TempDir(), "file.part"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	if err := f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	return f
}

func readAll(t *testing.T, f *os.File) []byte {
	t.Helper()
	b, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestFetchRangesRebuildTheFile(t *testing.T) {
	content := testContent(1_000_003) // odd size so ranges are uneven
	srv := rangeServer(t, content, `"v1"`, "")
	f := tempFile(t, int64(len(content)))

	ranges, err := Plan(int64(len(content)), 64*1024, 7)
	if err != nil {
		t.Fatal(err)
	}
	var reported atomic.Int64
	// Download back to front: offsets, not order, must decide where bytes land.
	for i := len(ranges) - 1; i >= 0; i-- {
		n, err := (&Fetcher{}).FetchRange(context.Background(), srv.URL, ranges[i], `"v1"`, f,
			func(n int64) { reported.Add(n) })
		if err != nil {
			t.Fatalf("range %d %+v: %v", i, ranges[i], err)
		}
		if n != ranges[i].Len() {
			t.Fatalf("range %d wrote %d bytes, want %d", i, n, ranges[i].Len())
		}
	}
	if !bytes.Equal(readAll(t, f), content) {
		t.Fatal("rebuilt file differs from the original")
	}
	if reported.Load() != int64(len(content)) {
		t.Errorf("progress reported %d bytes, want %d", reported.Load(), len(content))
	}
}

func TestFetchRangeRemoteChanged(t *testing.T) {
	srv := rangeServer(t, testContent(100), `"v2"`, "")
	f := tempFile(t, 100)
	_, err := (&Fetcher{}).FetchRange(context.Background(), srv.URL, Range{0, 49}, `"v1"`, f, nil)
	if !errors.Is(err, ErrRemoteChanged) {
		t.Fatalf("err = %v, want ErrRemoteChanged", err)
	}
}

func TestFetchRangeNotSupported(t *testing.T) {
	srv := noRangeServer(t, testContent(100))
	f := tempFile(t, 100)
	n, err := (&Fetcher{}).FetchRange(context.Background(), srv.URL, Range{0, 49}, "", f, nil)
	if !errors.Is(err, ErrRangeNotSupported) {
		t.Fatalf("err = %v, want ErrRangeNotSupported", err)
	}
	if n != 0 {
		t.Errorf("wrote %d bytes of a response that is the wrong data", n)
	}
}

func TestFetchRangeConnectionCut(t *testing.T) {
	content := testContent(100_000)
	srv := cutServer(t, content, 30_000)
	f := tempFile(t, int64(len(content)))

	n, err := (&Fetcher{}).FetchRange(context.Background(), srv.URL, Range{0, int64(len(content)) - 1}, "", f, nil)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("err = %v, want io.ErrUnexpectedEOF", err)
	}
	if n != 30_000 {
		t.Fatalf("n = %d, want 30000 so the download can resume from there", n)
	}
	if !bytes.Equal(readAll(t, f)[:n], content[:n]) {
		t.Error("the bytes that did arrive are wrong")
	}
}

func TestFetchRangeStallMidway(t *testing.T) {
	content := testContent(50_000)
	srv := stallServer(t, content, 10_000)
	f := tempFile(t, int64(len(content)))

	start := time.Now()
	n, err := (&Fetcher{IdleTimeout: 200 * time.Millisecond}).FetchRange(context.Background(), srv.URL,
		Range{0, int64(len(content)) - 1}, "", f, nil)
	if !errors.Is(err, ErrStalled) {
		t.Fatalf("err = %v, want ErrStalled", err)
	}
	if n != 10_000 {
		t.Errorf("n = %d, want 10000", n)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("took %v to notice the stall", d)
	}
}

// A slow but steady download must not be killed: the old server gave every
// chunk 30 s in total, which failed any chunk slower than ~1 MB/s.
func TestFetchRangeSlowButSteadyIsNotAStall(t *testing.T) {
	content := testContent(20_000)
	srv := slowServer(t, content, 1000, 50*time.Millisecond) // 20 pieces, ~1 s in total
	f := tempFile(t, int64(len(content)))

	fetcher := &Fetcher{IdleTimeout: 200 * time.Millisecond} // far less than the total time
	n, err := fetcher.FetchRange(context.Background(), srv.URL, Range{0, int64(len(content)) - 1}, "", f, nil)
	if err != nil {
		t.Fatalf("slow download failed after %d bytes: %v", n, err)
	}
	if !bytes.Equal(readAll(t, f), content) {
		t.Error("file differs from the original")
	}
}

func TestFetchRangeCanceled(t *testing.T) {
	content := testContent(50_000)
	srv := stallServer(t, content, 1_000)
	f := tempFile(t, int64(len(content)))

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	_, err := (&Fetcher{IdleTimeout: 5 * time.Second}).FetchRange(ctx, srv.URL, Range{0, int64(len(content)) - 1}, "", f, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled (a pause, not a stall)", err)
	}
}

func TestFetchRangeStatusError(t *testing.T) {
	srv := statusServer(t, 503)
	f := tempFile(t, 10)
	_, err := (&Fetcher{}).FetchRange(context.Background(), srv.URL, Range{0, 9}, "", f, nil)
	var se *StatusError
	if !errors.As(err, &se) || !se.Retryable() {
		t.Fatalf("err = %v, want a retryable *StatusError", err)
	}
}

func TestFetchRangeInvalid(t *testing.T) {
	f := tempFile(t, 10)
	for _, r := range []Range{{-1, 5}, {5, 4}} {
		if _, err := (&Fetcher{}).FetchRange(context.Background(), "http://unused", r, "", f, nil); err == nil {
			t.Errorf("FetchRange(%+v) accepted an invalid range", r)
		}
	}
}

func TestFetchWhole(t *testing.T) {
	content := testContent(300_000)
	for name, srv := range map[string]string{
		"no ranges":    noRangeServer(t, content).URL,
		"unknown size": unknownSizeServer(t, content).URL,
		"ranges":       rangeServer(t, content, "", "").URL,
	} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			var reported int64
			n, err := (&Fetcher{}).FetchWhole(context.Background(), srv, &buf, func(n int64) { reported += n })
			if err != nil {
				t.Fatal(err)
			}
			if n != int64(len(content)) || reported != n || !bytes.Equal(buf.Bytes(), content) {
				t.Errorf("n=%d reported=%d equal=%v", n, reported, bytes.Equal(buf.Bytes(), content))
			}
		})
	}
}

func TestFetchWholeConnectionCut(t *testing.T) {
	content := testContent(100_000)
	srv := cutServer(t, content, 40_000)
	var buf bytes.Buffer
	_, err := (&Fetcher{}).FetchWhole(context.Background(), srv.URL, &buf, nil)
	if err == nil {
		t.Fatal("a cut connection was reported as a complete download")
	}
}

func BenchmarkFetchRange(b *testing.B) {
	content := testContent(8 << 20)
	srv := rangeServer(b, content, "", "")
	f, err := os.Create(filepath.Join(b.TempDir(), "bench.part"))
	if err != nil {
		b.Fatal(err)
	}
	defer f.Close()
	r := Range{0, int64(len(content)) - 1}
	b.SetBytes(r.Len())
	for b.Loop() {
		if _, err := (&Fetcher{}).FetchRange(context.Background(), srv.URL, r, "", f, nil); err != nil {
			b.Fatal(err)
		}
	}
}
