// Package download is the CatchMe download engine. It knows nothing about
// WebSockets or the UI: it takes URLs and produces files and events.
package download

import "errors"

// Range is an inclusive byte range, the same convention as the HTTP
// Range header: Range{Start: 0, End: 99} is "bytes=0-99", 100 bytes.
type Range struct {
	Start int64
	End   int64
}

// Errors returned by Plan.
var (
	ErrInvalidSize = errors.New("download: size must be greater than zero")
	ErrInvalidArgs = errors.New("download: minPart and maxParts must be greater than zero")
)

// Len returns how many bytes the range covers.
func (r Range) Len() int64 {
	return r.End - r.Start + 1
}

// Plan splits a file of size bytes into contiguous ranges for parallel
// download:
//
//   - The ranges cover [0, size-1] exactly: no gaps, no overlaps, in order.
//   - The number of ranges is size/minPart (integer division), capped at
//     maxParts, and never less than 1. So every range has at least minPart
//     bytes, unless the whole file is smaller than that.
//   - Ranges are balanced: their lengths differ by at most 1 byte, and the
//     longer ones come first.
//   - size <= 0 returns ErrInvalidSize; minPart <= 0 or maxParts <= 0
//     returns ErrInvalidArgs.
func Plan(size, minPart int64, maxParts int) ([]Range, error) {
	if size <= 0 {
		return nil, ErrInvalidSize
	}
	if minPart <= 0 || maxParts <= 0 {
		return nil, ErrInvalidArgs
	}

	n := max(min(size/minPart, int64(maxParts)), 1)
	base, extra := size/n, size%n // the first `extra` ranges get one more byte

	ranges := make([]Range, n)
	var start int64
	for i := range ranges {
		length := base
		if int64(i) < extra {
			length++
		}
		ranges[i] = Range{Start: start, End: start + length - 1}
		start += length
	}
	return ranges, nil
}
