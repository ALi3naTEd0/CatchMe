package download

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ALi3naTEd0/CatchMe/catchmed/internal/events"
)

// Status is where a download is in its life.
type Status string

const (
	StatusQueued    Status = "queued"    // waiting for a free slot
	StatusRunning   Status = "running"   // downloading
	StatusPaused    Status = "paused"    // stopped by the user, progress kept
	StatusCompleted Status = "completed" // file in place and verified
	StatusFailed    Status = "failed"    // stopped by an error, progress kept
)

var (
	ErrNotFound   = errors.New("download: no download with that id")
	ErrNotAllowed = errors.New("download: not possible in the current status")

	errPauseRequested  = errors.New("paused by the user")
	errCancelRequested = errors.New("canceled by the user")
)

// Snapshot is a download as the UI sees it. It is also what the manager
// saves to disk, so the list survives a restart.
type Snapshot struct {
	ID         string    `json:"id"`
	URL        string    `json:"url"`
	Name       string    `json:"name,omitempty"`
	Path       string    `json:"path,omitempty"`
	Status     Status    `json:"status"`
	Done       int64     `json:"done"`
	Total      int64     `json:"total"`           // -1 while unknown
	Speed      float64   `json:"speed,omitempty"` // bytes per second, while running
	SHA256     string    `json:"sha256,omitempty"`
	Error      string    `json:"error,omitempty"`
	AddedAt    time.Time `json:"addedAt"`
	FinishedAt time.Time `json:"finishedAt,omitzero"`
}

// EventKind says what an Event is about.
type EventKind string

const (
	EventAdded    EventKind = "added"    // a new download is in the list
	EventChanged  EventKind = "changed"  // status changed (running, paused, completed...)
	EventProgress EventKind = "progress" // bytes or speed changed while running
	EventRemoved  EventKind = "removed"  // gone from the list
)

// Event is one change, carrying the whole download so a client never needs
// to merge partial updates.
type Event struct {
	Kind     EventKind `json:"kind"`
	Download Snapshot  `json:"download"`
}

// Config sets up a Manager. Zero fields take defaults.
type Config struct {
	Dir           string        // where finished files go (required)
	StateDir      string        // where the list and progress are kept (required)
	Fetcher       *Fetcher      // nil: 30 s idle timeout
	Options       Options       // per-download options
	MaxActive     int           // downloads running at the same time (3)
	ProgressEvery time.Duration // how often progress events go out (250 ms)
}

// Manager runs a list of downloads: a queue with a limit on how many run at
// once, pause, resume and cancel, progress events, and a list that survives
// restarts. All methods are safe to call from any goroutine.
type Manager struct {
	cfg  Config
	bus  events.Bus[Event]
	wake chan struct{}

	mu       sync.Mutex
	items    map[string]*item
	order    []string // ids in the order they were added
	stopping bool
	running  sync.WaitGroup
}

type item struct {
	snap   Snapshot
	task   *Task                   // while running
	cancel context.CancelCauseFunc // while running
	fresh  bool                    // discard partial data on the next start

	lastDone int64
	lastAt   time.Time
}

// NewManager creates a Manager and loads the list saved in cfg.StateDir.
// Downloads that were running when the program stopped come back queued.
func NewManager(cfg Config) (*Manager, error) {
	if cfg.Dir == "" || cfg.StateDir == "" {
		return nil, errors.New("download: Config.Dir and Config.StateDir are required")
	}
	if cfg.Fetcher == nil {
		cfg.Fetcher = &Fetcher{IdleTimeout: 30 * time.Second}
	}
	if cfg.MaxActive <= 0 {
		cfg.MaxActive = 3
	}
	if cfg.ProgressEvery <= 0 {
		cfg.ProgressEvery = 250 * time.Millisecond
	}
	if err := os.MkdirAll(cfg.StateDir, 0o755); err != nil {
		return nil, fmt.Errorf("download: %w", err)
	}
	m := &Manager{cfg: cfg, wake: make(chan struct{}, 1), items: make(map[string]*item)}
	if err := m.load(); err != nil {
		return nil, err
	}
	return m, nil
}

// Run starts queued downloads and sends progress until ctx ends. Then it
// stops the running downloads, keeping their progress so they continue
// queued on the next start, and returns once they have all stopped.
func (m *Manager) Run(ctx context.Context) error {
	tick := time.NewTicker(m.cfg.ProgressEvery)
	defer tick.Stop()
	for {
		m.schedule(ctx)
		select {
		case <-ctx.Done():
			m.mu.Lock()
			m.stopping = true
			m.mu.Unlock()
			m.running.Wait() // their contexts derive from ctx, so they are stopping already
			m.bus.Close()
			return nil
		case <-m.wake:
		case <-tick.C:
			m.publishProgress()
		}
	}
}

// Add queues a new download and returns its id.
func (m *Manager) Add(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if !isHTTPURL(rawURL) {
		return "", fmt.Errorf("%w: %q", ErrUnsupportedURL, rawURL)
	}
	it := &item{snap: Snapshot{ID: newID(), URL: rawURL, Status: StatusQueued, Total: -1, AddedAt: time.Now().UTC()}}
	m.mu.Lock()
	m.items[it.snap.ID] = it
	m.order = append(m.order, it.snap.ID)
	m.persist(it)
	m.bus.Publish(Event{Kind: EventAdded, Download: it.snap})
	m.mu.Unlock()
	m.poke()
	return it.snap.ID, nil
}

// Pause stops a running or queued download, keeping what it downloaded.
func (m *Manager) Pause(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.items[id]
	if !ok {
		return ErrNotFound
	}
	switch it.snap.Status {
	case StatusRunning:
		it.cancel(errPauseRequested) // finish() records the new status
	case StatusQueued:
		m.setStatus(it, StatusPaused)
	default:
		return fmt.Errorf("%w: cannot pause a %s download", ErrNotAllowed, it.snap.Status)
	}
	return nil
}

// Resume queues a paused or failed download again. It continues where it
// stopped unless the file changed on the server, which starts it over.
func (m *Manager) Resume(id string) error {
	m.mu.Lock()
	it, ok := m.items[id]
	if !ok {
		m.mu.Unlock()
		return ErrNotFound
	}
	if it.snap.Status != StatusPaused && it.snap.Status != StatusFailed {
		m.mu.Unlock()
		return fmt.Errorf("%w: cannot resume a %s download", ErrNotAllowed, it.snap.Status)
	}
	it.snap.Error = ""
	m.setStatus(it, StatusQueued)
	m.mu.Unlock()
	m.poke()
	return nil
}

// Cancel stops a download that is not finished and deletes its partial
// data. Use Remove for finished downloads.
func (m *Manager) Cancel(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.items[id]
	if !ok {
		return ErrNotFound
	}
	switch it.snap.Status {
	case StatusRunning:
		it.cancel(errCancelRequested) // finish() deletes and removes it
	case StatusCompleted:
		return fmt.Errorf("%w: the download already finished; use Remove", ErrNotAllowed)
	default:
		m.discardPartial(it.snap.ID)
		m.forget(it)
	}
	return nil
}

// Remove takes a finished or failed download off the list. The downloaded
// file is kept; a failed download's partial data is deleted.
func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.items[id]
	if !ok {
		return ErrNotFound
	}
	if it.snap.Status != StatusCompleted && it.snap.Status != StatusFailed {
		return fmt.Errorf("%w: cannot remove a %s download; pause or cancel it first", ErrNotAllowed, it.snap.Status)
	}
	if it.snap.Status == StatusFailed {
		m.discardPartial(it.snap.ID)
	}
	m.forget(it)
	return nil
}

// List returns every download in the order they were added.
func (m *Manager) List() []Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.list()
}

// Subscribe returns the current list and a subscription to every change
// after it, taken atomically so nothing falls in between. buffer is how many
// events the subscriber may fall behind before it is dropped (see
// events.Subscription.Lagged); then it should call Subscribe again.
func (m *Manager) Subscribe(buffer int) ([]Snapshot, *events.Subscription[Event]) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.list(), m.bus.Subscribe(buffer)
}

func (m *Manager) list() []Snapshot {
	out := make([]Snapshot, 0, len(m.order))
	for _, id := range m.order {
		out = append(out, m.items[id].snap)
	}
	return out
}

// schedule starts queued downloads, oldest first, while there are free slots.
func (m *Manager) schedule(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopping || ctx.Err() != nil {
		return
	}
	active := 0
	for _, it := range m.items {
		if it.snap.Status == StatusRunning {
			active++
		}
	}
	for _, id := range m.order {
		if active >= m.cfg.MaxActive {
			return
		}
		if it := m.items[id]; it.snap.Status == StatusQueued {
			m.start(ctx, it)
			active++
		}
	}
}

// start launches it in its own goroutine. Called with m.mu held.
func (m *Manager) start(ctx context.Context, it *item) {
	id := it.snap.ID
	statePath := m.statePath(id)
	var task *Task
	if !it.fresh {
		if st, err := LoadState(statePath); err == nil {
			task = Resume(st, m.cfg.Fetcher, m.cfg.Options, statePath)
		}
	}
	if task == nil {
		m.discardPartial(id)
		task = &Task{URL: it.snap.URL, Dir: m.cfg.Dir, Fetcher: m.cfg.Fetcher, Options: m.cfg.Options, StatePath: statePath}
	}
	it.fresh = false

	tctx, cancel := context.WithCancelCause(ctx)
	it.task, it.cancel = task, cancel
	it.lastDone, it.lastAt = it.snap.Done, time.Now()
	m.setStatus(it, StatusRunning)

	m.running.Add(1)
	go func() {
		defer m.running.Done()
		res, err := task.Run(tctx)
		cancel(nil)
		m.finish(id, res, err)
	}()
}

// finish records how a download's run ended.
func (m *Manager) finish(id string, res Result, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	it, ok := m.items[id]
	if !ok {
		return
	}
	m.refresh(it)
	it.task, it.cancel = nil, nil
	it.snap.Speed = 0

	switch {
	case err == nil:
		it.snap.Path, it.snap.Name = res.Path, filepath.Base(res.Path)
		it.snap.Done, it.snap.Total, it.snap.SHA256 = res.Size, res.Size, res.SHA256
		it.snap.FinishedAt = time.Now().UTC()
		m.setStatus(it, StatusCompleted)
	case errors.Is(err, errCancelRequested):
		m.discardPartial(id)
		m.forget(it)
	case errors.Is(err, errPauseRequested):
		m.setStatus(it, StatusPaused)
	case m.stopping:
		m.setStatus(it, StatusQueued) // the program is closing: continue on the next start
	default:
		it.snap.Error = err.Error()
		// A changed file or an unusable state cannot be continued: the next
		// Resume starts from zero.
		it.fresh = errors.Is(err, ErrRemoteChanged) || errors.Is(err, ErrStateInvalid)
		m.setStatus(it, StatusFailed)
	}
	if !m.stopping {
		m.poke() // a slot is free
	}
}

// publishProgress sends a progress event for every running download whose
// numbers moved, with its speed smoothed so the UI does not flicker.
func (m *Manager) publishProgress() {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for _, id := range m.order {
		it := m.items[id]
		if it.task == nil {
			continue
		}
		before := it.snap
		m.refresh(it)
		if dt := now.Sub(it.lastAt).Seconds(); dt > 0 {
			instant := float64(it.snap.Done-it.lastDone) / dt
			if it.snap.Speed == 0 {
				it.snap.Speed = instant
			} else {
				it.snap.Speed = 0.3*instant + 0.7*it.snap.Speed
			}
		}
		it.lastDone, it.lastAt = it.snap.Done, now
		if it.snap != before {
			m.bus.Publish(Event{Kind: EventProgress, Download: it.snap})
		}
	}
}

// refresh copies the running task's numbers and file name into the snapshot.
func (m *Manager) refresh(it *item) {
	if it.task == nil {
		return
	}
	it.snap.Done, it.snap.Total = it.task.Progress()
	if p := it.task.Snapshot().Path; p != "" {
		it.snap.Path, it.snap.Name = p, filepath.Base(p)
	}
}

func (m *Manager) setStatus(it *item, s Status) {
	it.snap.Status = s
	m.persist(it)
	m.bus.Publish(Event{Kind: EventChanged, Download: it.snap})
}

func (m *Manager) forget(it *item) {
	id := it.snap.ID
	delete(m.items, id)
	m.order = slices.DeleteFunc(m.order, func(x string) bool { return x == id })
	_ = os.Remove(m.recordPath(id))
	m.bus.Publish(Event{Kind: EventRemoved, Download: it.snap})
}

func (m *Manager) poke() {
	select {
	case m.wake <- struct{}{}:
	default: // a wake-up is already pending
	}
}

// Files in StateDir: <id>.json is the Snapshot, <id>.state.json the Task progress.
func (m *Manager) recordPath(id string) string { return filepath.Join(m.cfg.StateDir, id+".json") }
func (m *Manager) statePath(id string) string  { return filepath.Join(m.cfg.StateDir, id+".state.json") }

// persist saves the snapshot. A failure is reported in the snapshot instead
// of stopping the download: the file matters more than the list entry.
func (m *Manager) persist(it *item) {
	snap := it.snap
	snap.Speed = 0
	if err := writeJSON(m.recordPath(snap.ID), snap); err != nil && it.snap.Error == "" {
		it.snap.Error = err.Error()
	}
}

// discardPartial deletes a download's .part file and saved progress. It reads
// the state loosely, since it may be the invalid state being thrown away.
func (m *Manager) discardPartial(id string) {
	path := m.statePath(id)
	if b, err := os.ReadFile(path); err == nil {
		var st State
		if json.Unmarshal(b, &st) == nil && st.Path != "" {
			_ = os.Remove(st.Path + ".part")
		}
	}
	_ = os.Remove(path)
}

// load reads the saved list. Unreadable records are skipped, not fatal.
func (m *Manager) load() error {
	entries, err := os.ReadDir(m.cfg.StateDir)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	var snaps []Snapshot
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".state.json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(m.cfg.StateDir, name))
		if err != nil {
			continue
		}
		var s Snapshot
		if json.Unmarshal(b, &s) != nil || s.ID == "" || s.ID+".json" != name {
			continue
		}
		snaps = append(snaps, s)
	}
	slices.SortFunc(snaps, func(a, b Snapshot) int { return a.AddedAt.Compare(b.AddedAt) })
	for _, s := range snaps {
		if s.Status == StatusRunning {
			s.Status = StatusQueued
		}
		if st, err := LoadState(m.statePath(s.ID)); err == nil {
			s.Done, s.Total = st.Done(), st.Size
			if st.Path != "" {
				s.Path, s.Name = st.Path, filepath.Base(st.Path)
			}
		}
		m.items[s.ID] = &item{snap: s}
		m.order = append(m.order, s.ID)
	}
	return nil
}

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
