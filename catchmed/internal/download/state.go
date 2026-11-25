package download

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrStateInvalid means a saved state cannot be resumed: wrong version,
// missing or resized .part file, or ranges that do not add up.
var ErrStateInvalid = errors.New("download: saved state cannot be resumed")

const stateVersion = 1

// State is everything needed to continue a download after a pause or a
// crash. It is saved as JSON; the downloaded bytes live in Path + ".part".
type State struct {
	Version   int          `json:"version"`
	URL       string       `json:"url"`  // as the user gave it; re-probed on resume
	Path      string       `json:"path"` // final file name, reserved when the download started
	Size      int64        `json:"size"` // -1 if unknown
	Validator string       `json:"validator,omitempty"`
	Parallel  bool         `json:"parallel"` // false: one stream, which restarts from zero
	Ranges    []RangeState `json:"ranges,omitempty"`
}

// RangeState is one range and how many of its bytes are already on disk.
type RangeState struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`
	Done  int64 `json:"done"`
}

// Done returns the bytes already downloaded according to the state.
func (s State) Done() int64 {
	var n int64
	for _, r := range s.Ranges {
		n += r.Done
	}
	return n
}

// check verifies that a parallel state's ranges cover [0, Size-1] exactly and
// that no range claims more bytes than it has.
func (s State) check() error {
	if s.Version != stateVersion {
		return fmt.Errorf("%w: version %d, want %d", ErrStateInvalid, s.Version, stateVersion)
	}
	if s.Path == "" || s.URL == "" {
		return fmt.Errorf("%w: missing url or path", ErrStateInvalid)
	}
	if !s.Parallel {
		return nil
	}
	if s.Size <= 0 || len(s.Ranges) == 0 {
		return fmt.Errorf("%w: parallel download without size or ranges", ErrStateInvalid)
	}
	var next int64
	for i, r := range s.Ranges {
		rng := Range{r.Start, r.End}
		if r.Start != next || r.End < r.Start || r.Done < 0 || r.Done > rng.Len() {
			return fmt.Errorf("%w: range %d %+v", ErrStateInvalid, i, r)
		}
		next = r.End + 1
	}
	if next != s.Size {
		return fmt.Errorf("%w: ranges cover %d bytes, file has %d", ErrStateInvalid, next, s.Size)
	}
	return nil
}

// SaveState writes st to path atomically: a crash in the middle leaves the
// previous state intact instead of a half-written JSON file.
func SaveState(path string, st State) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp*")
	if err != nil {
		return fmt.Errorf("download: save state: %w", err)
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return fmt.Errorf("download: save state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("download: save state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("download: save state: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("download: save state: %w", err)
	}
	return nil
}

// LoadState reads a state saved by SaveState and checks it is usable.
func LoadState(path string) (State, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return State{}, fmt.Errorf("download: load state: %w", err)
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return State{}, fmt.Errorf("%w: %v", ErrStateInvalid, err)
	}
	if err := st.check(); err != nil {
		return State{}, err
	}
	return st, nil
}
