package main

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain fails the run if any test leaves a goroutine behind: a download,
// a watchdog or a WebSocket loop that never stopped.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
