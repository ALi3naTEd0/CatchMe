package download

import (
	"errors"
	"slices"
	"testing"
)

const (
	KiB = int64(1) << 10
	MiB = int64(1) << 20
	GiB = int64(1) << 30
)

func TestRangeLen(t *testing.T) {
	tests := []struct {
		r    Range
		want int64
	}{
		{Range{0, 0}, 1},
		{Range{0, 99}, 100},
		{Range{100, 199}, 100},
		{Range{5, 6}, 2},
	}
	for _, tt := range tests {
		if got := tt.r.Len(); got != tt.want {
			t.Errorf("%+v.Len() = %d, want %d", tt.r, got, tt.want)
		}
	}
}

func TestPlan(t *testing.T) {
	tests := []struct {
		name     string
		size     int64
		minPart  int64
		maxParts int
		want     []Range
	}{
		{"even split", 100, 10, 4, []Range{{0, 24}, {25, 49}, {50, 74}, {75, 99}}},
		{"uneven split puts the extra bytes first", 7, 1, 3, []Range{{0, 2}, {3, 4}, {5, 6}}},
		{"exactly one part", 10, 10, 4, []Range{{0, 9}}},
		{"smaller than minPart still gives one part", 3, 10, 4, []Range{{0, 2}}},
		{"one byte", 1, 1, 8, []Range{{0, 0}}},
		{"minPart limits the count", 25, 10, 8, []Range{{0, 12}, {13, 24}}},
		{"maxParts limits the count", 1000, 1, 3, []Range{{0, 333}, {334, 666}, {667, 999}}},
		{"maxParts of one", 500, 10, 1, []Range{{0, 499}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Plan(tt.size, tt.minPart, tt.maxParts)
			if err != nil {
				t.Fatalf("Plan(%d, %d, %d) error: %v", tt.size, tt.minPart, tt.maxParts, err)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("Plan(%d, %d, %d)\n got  %v\n want %v", tt.size, tt.minPart, tt.maxParts, got, tt.want)
			}
		})
	}
}

func TestPlanErrors(t *testing.T) {
	tests := []struct {
		name     string
		size     int64
		minPart  int64
		maxParts int
		want     error
	}{
		{"zero size", 0, 10, 4, ErrInvalidSize},
		{"negative size", -1, 10, 4, ErrInvalidSize},
		{"zero minPart", 100, 0, 4, ErrInvalidArgs},
		{"negative minPart", 100, -5, 4, ErrInvalidArgs},
		{"zero maxParts", 100, 10, 0, ErrInvalidArgs},
		{"size is checked first", 0, 0, 0, ErrInvalidSize},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Plan(tt.size, tt.minPart, tt.maxParts)
			if !errors.Is(err, tt.want) {
				t.Fatalf("Plan(%d, %d, %d) error = %v, want %v", tt.size, tt.minPart, tt.maxParts, err, tt.want)
			}
			if got != nil {
				t.Errorf("on error Plan should return nil ranges, got %v", got)
			}
		})
	}
}

// checkPlan verifies every rule from Plan's doc comment, for any input.
func checkPlan(t *testing.T, size, minPart int64, maxParts int, got []Range) {
	t.Helper()
	wantN := min(int64(maxParts), size/minPart)
	wantN = max(wantN, 1)
	if int64(len(got)) != wantN {
		t.Fatalf("size=%d minPart=%d maxParts=%d: got %d ranges, want %d", size, minPart, maxParts, len(got), wantN)
	}
	if got[0].Start != 0 {
		t.Fatalf("first range starts at %d, want 0", got[0].Start)
	}
	if last := got[len(got)-1]; last.End != size-1 {
		t.Fatalf("last range ends at %d, want %d", last.End, size-1)
	}
	var total int64
	for i, r := range got {
		if r.Start > r.End {
			t.Fatalf("range %d is empty or reversed: %+v", i, r)
		}
		if i > 0 && r.Start != got[i-1].End+1 {
			t.Fatalf("gap or overlap between range %d %+v and %d %+v", i-1, got[i-1], i, r)
		}
		if i > 0 && r.Len() > got[i-1].Len() {
			t.Fatalf("range %d (%d bytes) is longer than range %d (%d bytes): longer ones go first", i, r.Len(), i-1, got[i-1].Len())
		}
		total += r.Len()
	}
	if total != size {
		t.Fatalf("ranges cover %d bytes, want %d", total, size)
	}
	if d := got[0].Len() - got[len(got)-1].Len(); d > 1 {
		t.Fatalf("ranges are unbalanced: longest %d, shortest %d", got[0].Len(), got[len(got)-1].Len())
	}
}

func TestPlanRealisticSizes(t *testing.T) {
	sizes := []int64{1, 999, 32 * MiB, 32*MiB + 1, 700 * MiB, 4*GiB + 7, 80 * GiB}
	for _, size := range sizes {
		for _, maxParts := range []int{1, 4, 8, 16} {
			got, err := Plan(size, 8*MiB, maxParts)
			if err != nil {
				t.Fatalf("Plan(%d, 8MiB, %d): %v", size, maxParts, err)
			}
			checkPlan(t, size, 8*MiB, maxParts, got)
		}
	}
}

// FuzzPlan throws random inputs at Plan. Run it with:
//
//	go test ./internal/download -fuzz FuzzPlan -fuzztime 30s
func FuzzPlan(f *testing.F) {
	f.Add(int64(100), int64(10), 4)
	f.Add(int64(7), int64(1), 3)
	f.Add(int64(80*GiB), int64(8*MiB), 16)
	f.Fuzz(func(t *testing.T, size, minPart int64, maxParts int) {
		if size <= 0 || minPart <= 0 || maxParts <= 0 || maxParts > 1024 {
			t.Skip()
		}
		got, err := Plan(size, minPart, maxParts)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		checkPlan(t, size, minPart, maxParts, got)
	})
}

func BenchmarkPlan(b *testing.B) {
	for b.Loop() {
		_, _ = Plan(80*GiB, 8*MiB, 16)
	}
}
