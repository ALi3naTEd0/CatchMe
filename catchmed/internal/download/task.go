package download

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"
)

// ErrUnsupportedURL is returned for anything that is not an http or https URL.
var ErrUnsupportedURL = errors.New("download: only http and https URLs are supported")

// Options tunes a Task. Zero fields take the defaults below.
type Options struct {
	MinPart    int64         // smallest range worth its own connection (8 MiB)
	MaxParts   int           // parallel connections per download (8)
	MaxRetries int           // failed attempts in a row before giving up on a range (5)
	RetryBase  time.Duration // first wait between attempts, doubled each time (500 ms)
	RetryMax   time.Duration // longest wait between attempts (15 s)
	SaveEvery  time.Duration // how often progress is saved to StatePath (1 s)
}

func (o Options) withDefaults() Options {
	if o.MinPart <= 0 {
		o.MinPart = 8 << 20
	}
	if o.MaxParts <= 0 {
		o.MaxParts = 8
	}
	if o.MaxRetries <= 0 {
		o.MaxRetries = 5
	}
	if o.RetryBase <= 0 {
		o.RetryBase = 500 * time.Millisecond
	}
	if o.RetryMax <= 0 {
		o.RetryMax = 15 * time.Second
	}
	if o.SaveEvery <= 0 {
		o.SaveEvery = time.Second
	}
	return o
}

// Result describes a finished download.
type Result struct {
	Path   string // where the file ended up
	Size   int64
	SHA256 string // hex
}

// Task is one download from a URL into a directory. Create it with the
// exported fields set and call Run, or rebuild it from a saved State with
// Resume. Pausing is canceling Run's context: progress stays on disk.
type Task struct {
	URL     string
	Dir     string
	Fetcher *Fetcher // nil uses a Fetcher with a 30 s idle timeout
	Options Options
	// StatePath, if set, is where progress is saved while Run works and when
	// it stops, so Resume can continue after a pause or a crash. It is
	// removed when the download succeeds.
	StatePath string

	resume *State // set by Resume

	done  atomic.Int64 // bytes on disk so far
	total atomic.Int64 // file size, -1 while unknown

	mu    sync.Mutex // guards st and parts (the slice, not the counters)
	st    State
	parts []*partProgress
}

// partProgress is one range and how many of its bytes are on disk. done is
// updated by its download goroutine and read by the state saver.
type partProgress struct {
	r    Range
	done atomic.Int64
}

// Resume rebuilds a Task from a saved State. Run then re-checks the file on
// the server and downloads only what is missing.
func Resume(st State, f *Fetcher, opts Options, statePath string) *Task {
	return &Task{URL: st.URL, Dir: filepath.Dir(st.Path), Fetcher: f, Options: opts, StatePath: statePath, resume: &st}
}

// Progress returns the bytes downloaded so far and the total (-1 if unknown).
// It is safe to call from any goroutine while Run is working.
func (t *Task) Progress() (done, total int64) {
	return t.done.Load(), t.total.Load()
}

// Snapshot returns the current state, ready for SaveState. Safe to call while Run works.
func (t *Task) Snapshot() State {
	t.mu.Lock()
	defer t.mu.Unlock()
	st := t.st
	st.Ranges = make([]RangeState, len(t.parts))
	for i, p := range t.parts {
		st.Ranges[i] = RangeState{Start: p.r.Start, End: p.r.End, Done: p.done.Load()}
	}
	return st
}

// Run downloads the file and returns where it was saved. The file is written
// as "<name>.part" and renamed only when complete and verified, so a crash or
// cancel never leaves a file that looks finished but is not.
func (t *Task) Run(ctx context.Context) (Result, error) {
	if !isHTTPURL(t.URL) {
		return Result{}, fmt.Errorf("%w: %q", ErrUnsupportedURL, t.URL)
	}
	opts := t.Options.withDefaults()
	f := t.Fetcher
	if f == nil {
		f = &Fetcher{IdleTimeout: 30 * time.Second}
	}
	t.total.Store(-1)

	var info Info
	err := retry(ctx, opts, func() (int64, error) {
		var err error
		info, err = f.Probe(ctx, t.URL)
		return 0, err
	})
	if err != nil {
		return Result{}, err
	}

	var part *os.File
	if t.resume != nil {
		part, err = t.reopen(info)
	} else {
		part, err = t.start(info, opts)
	}
	if err != nil {
		return Result{}, err
	}
	defer part.Close()
	t.total.Store(info.Size)

	stopSaving := t.saveEvery(part, opts.SaveEvery)
	err = t.fetch(ctx, f, info.URL, part, opts)
	stopSaving()
	if err != nil {
		// Paused, canceled or failed: keep the .part and record how far we got.
		if saveErr := t.save(part); saveErr != nil {
			err = errors.Join(err, saveErr)
		}
		return Result{}, err
	}

	sum, size, err := finish(part, t.Snapshot().Size)
	if err != nil {
		return Result{}, err
	}
	finalPath := t.Snapshot().Path
	if err := os.Rename(part.Name(), finalPath); err != nil {
		return Result{}, fmt.Errorf("download: %w", err)
	}
	if t.StatePath != "" {
		_ = os.Remove(t.StatePath)
	}
	return Result{Path: finalPath, Size: size, SHA256: sum}, nil
}

// start reserves a file name and, when the server allows it, preallocates
// the file and plans the ranges.
func (t *Task) start(info Info, opts Options) (*os.File, error) {
	finalPath, part, err := reserve(t.Dir, info.Filename)
	if err != nil {
		return nil, err
	}
	st := State{Version: stateVersion, URL: t.URL, Path: finalPath, Size: info.Size,
		Validator: info.Validator, Parallel: info.Ranges && info.Size > 0}
	var parts []*partProgress
	if st.Parallel {
		if err := part.Truncate(info.Size); err != nil {
			part.Close()
			return nil, fmt.Errorf("download: preallocate: %w", err)
		}
		ranges, err := Plan(info.Size, opts.MinPart, opts.MaxParts)
		if err != nil {
			part.Close()
			return nil, err
		}
		for _, r := range ranges {
			parts = append(parts, &partProgress{r: r})
		}
	}
	t.mu.Lock()
	t.st, t.parts = st, parts
	t.mu.Unlock()
	return part, nil
}

// reopen continues from t.resume after checking that the server still has
// the same file and the .part on disk matches the saved state.
func (t *Task) reopen(info Info) (*os.File, error) {
	st := *t.resume
	if err := st.check(); err != nil {
		return nil, err
	}
	if _, err := os.Lstat(st.Path); err == nil {
		return nil, fmt.Errorf("%w: %s already exists", ErrStateInvalid, st.Path)
	}
	part, err := os.OpenFile(st.Path+".part", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrStateInvalid, err)
	}

	var parts []*partProgress
	if st.Parallel {
		if info.Size != st.Size || !info.Ranges || (st.Validator != "" && info.Validator != st.Validator) {
			part.Close()
			return nil, ErrRemoteChanged
		}
		if fi, err := part.Stat(); err != nil || fi.Size() != st.Size {
			part.Close()
			return nil, fmt.Errorf("%w: .part file has the wrong size", ErrStateInvalid)
		}
		for _, r := range st.Ranges {
			p := &partProgress{r: Range{r.Start, r.End}}
			p.done.Store(r.Done)
			parts = append(parts, p)
		}
		t.done.Store(st.Done())
	}
	st.Size = info.Size // a sequential download may learn its size now
	t.mu.Lock()
	t.st, t.parts = st, parts
	t.mu.Unlock()
	return part, nil
}

// fetch downloads whatever is missing, in parallel when possible.
func (t *Task) fetch(ctx context.Context, f *Fetcher, rawURL string, part *os.File, opts Options) error {
	t.mu.Lock()
	parallel, size := t.st.Parallel, t.st.Size
	t.mu.Unlock()

	if parallel {
		err := t.fetchParallel(ctx, f, rawURL, part, opts)
		if !errors.Is(err, ErrRangeNotSupported) {
			return err
		}
		// The server said it supports ranges and then ignored them: start
		// over with a single stream instead of failing.
		t.mu.Lock()
		t.st.Parallel, t.parts = false, nil
		t.mu.Unlock()
	} else if size == 0 {
		return nil // empty file: nothing to download
	}
	return t.fetchSequential(ctx, f, rawURL, part, opts)
}

// fetchParallel downloads the missing bytes of every range, up to MaxParts
// at once. The first range that fails for good cancels the others.
func (t *Task) fetchParallel(ctx context.Context, f *Fetcher, rawURL string, part *os.File, opts Options) error {
	t.mu.Lock()
	parts, validator := t.parts, t.st.Validator
	t.mu.Unlock()

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(opts.MaxParts)
	for _, p := range parts {
		if p.done.Load() == p.r.Len() {
			continue // finished before a pause
		}
		g.Go(func() error {
			return retry(gctx, opts, func() (int64, error) {
				missing := Range{Start: p.r.Start + p.done.Load(), End: p.r.End}
				return f.FetchRange(gctx, rawURL, missing, validator, part, func(n int64) {
					p.done.Add(n)
					t.done.Add(n)
				})
			})
		})
	}
	return g.Wait()
}

// fetchSequential downloads the whole file in one stream. Without ranges
// there is no way to resume, so every retry (and every resume) starts from
// byte zero.
func (t *Task) fetchSequential(ctx context.Context, f *Fetcher, rawURL string, part *os.File, opts Options) error {
	return retry(ctx, opts, func() (int64, error) {
		if err := restart(part); err != nil {
			return 0, err
		}
		t.done.Store(0)
		n, err := f.FetchWhole(ctx, rawURL, part, func(n int64) { t.done.Add(n) })
		if err == nil {
			t.total.Store(n) // now the size is known even if the server never said
			t.mu.Lock()
			t.st.Size = n
			t.mu.Unlock()
		}
		return 0, err // progress does not count: the next attempt starts over anyway
	})
}

// save flushes the downloaded bytes to disk and then records them in the
// state file, in that order: the state never claims bytes the disk lacks.
func (t *Task) save(part *os.File) error {
	if t.StatePath == "" {
		return nil
	}
	if err := part.Sync(); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	return SaveState(t.StatePath, t.Snapshot())
}

// saveEvery saves the state periodically until the returned stop is called.
// stop waits for the saver goroutine to exit.
func (t *Task) saveEvery(part *os.File, every time.Duration) (stop func()) {
	if t.StatePath == "" {
		return func() {}
	}
	quit, exited := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(exited)
		tick := time.NewTicker(every)
		defer tick.Stop()
		for {
			select {
			case <-tick.C:
				_ = t.save(part) // a failed periodic save is retried on the next tick and at the end
			case <-quit:
				return
			}
		}
	}()
	return func() {
		close(quit)
		<-exited
	}
}

func isHTTPURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func restart(f *os.File) error {
	if err := f.Truncate(0); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("download: %w", err)
	}
	return nil
}

// retry calls attempt until it succeeds, fails for good, or ctx ends. attempt
// returns how many bytes it made progress; any progress resets the count of
// failures, so a connection that keeps dropping but keeps advancing never
// runs out of retries.
func retry(ctx context.Context, opts Options, attempt func() (progress int64, err error)) error {
	failures := 0
	for {
		n, err := attempt()
		if err == nil {
			return nil
		}
		if ctx.Err() != nil {
			return context.Cause(ctx) // canceled or paused: not a failure
		}
		if !retryable(err) {
			return err
		}
		if n > 0 {
			failures = 0
		}
		failures++
		if failures > opts.MaxRetries {
			return fmt.Errorf("download: giving up after %d attempts: %w", failures, err)
		}
		if err := sleep(ctx, backoff(opts, failures, err)); err != nil {
			return err
		}
	}
}

// retryable separates errors that a later attempt can fix (network drops,
// stalls, 5xx, 429) from those it cannot (404, 403, a changed file).
func retryable(err error) bool {
	var se *StatusError
	if errors.As(err, &se) {
		return se.Retryable()
	}
	switch {
	case errors.Is(err, ErrRangeNotSupported), errors.Is(err, ErrRemoteChanged),
		errors.Is(err, ErrUnsupportedURL), errors.Is(err, ErrInvalidSize):
		return false
	}
	return true
}

// backoff doubles the wait on each failure up to RetryMax, with random
// jitter so parallel ranges do not retry in lockstep. A server's Retry-After
// wins when it asks for longer, up to two minutes.
func backoff(opts Options, failures int, err error) time.Duration {
	d := opts.RetryBase << min(failures-1, 20)
	d = min(d, opts.RetryMax)
	d = d/2 + rand.N(d/2+1) // between half and the full delay
	var se *StatusError
	if errors.As(err, &se) && se.RetryAfter > d {
		d = min(se.RetryAfter, 2*time.Minute)
	}
	return d
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

// reserve picks a free name in dir ("movie.mkv", then "movie (1).mkv", ...)
// and creates its .part file. O_EXCL makes the reservation atomic, so two
// downloads of files with the same name never write to the same place.
func reserve(dir, name string) (finalPath string, part *os.File, err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", nil, fmt.Errorf("download: %w", err)
	}
	base, ext := splitExt(name)
	for i := 0; i < 1000; i++ {
		candidate := name
		if i > 0 {
			candidate = fmt.Sprintf("%s (%d)%s", base, i, ext)
		}
		finalPath = filepath.Join(dir, candidate)
		if _, err := os.Lstat(finalPath); err == nil {
			continue // a finished file already has this name
		}
		part, err = os.OpenFile(finalPath+".part", os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue // another download is using this name
		}
		if err != nil {
			return "", nil, fmt.Errorf("download: %w", err)
		}
		return finalPath, part, nil
	}
	return "", nil, fmt.Errorf("download: no free name for %q in %s", name, dir)
}

// finish flushes the part file to disk, checks its size and hashes it.
// Ranges arrive out of order, so the hash needs one sequential pass at the end.
func finish(part *os.File, wantSize int64) (sum string, size int64, err error) {
	if err := part.Sync(); err != nil {
		return "", 0, fmt.Errorf("download: %w", err)
	}
	if _, err := part.Seek(0, io.SeekStart); err != nil {
		return "", 0, fmt.Errorf("download: %w", err)
	}
	h := sha256.New()
	size, err = io.Copy(h, part)
	if err != nil {
		return "", 0, fmt.Errorf("download: hashing: %w", err)
	}
	if wantSize >= 0 && size != wantSize {
		return "", 0, fmt.Errorf("download: got %d bytes, server announced %d", size, wantSize)
	}
	return hex.EncodeToString(h.Sum(nil)), size, nil
}
