package events

import (
	"sync"
	"testing"
)

func TestFanOutInOrder(t *testing.T) {
	var b Bus[int]
	a, c := b.Subscribe(10), b.Subscribe(10)
	for i := range 5 {
		b.Publish(i)
	}
	for _, s := range []*Subscription[int]{a, c} {
		for want := range 5 {
			if got := <-s.C(); got != want {
				t.Fatalf("got %d, want %d", got, want)
			}
		}
	}
}

func TestSlowSubscriberIsDroppedNotBlocking(t *testing.T) {
	var b Bus[int]
	slow, fast := b.Subscribe(2), b.Subscribe(100)
	for i := range 10 { // would block forever if Publish waited for slow
		b.Publish(i)
	}
	if !slow.Lagged() {
		t.Error("slow subscriber not marked as lagged")
	}
	n := 0
	for range slow.C() { // drains what fit, then the channel is closed
		n++
	}
	if n != 2 {
		t.Errorf("slow got %d values before being dropped, want its buffer of 2", n)
	}
	if fast.Lagged() || len(fast.C()) != 10 {
		t.Errorf("fast subscriber affected: lagged=%v queued=%d", fast.Lagged(), len(fast.C()))
	}
	if b.Len() != 1 {
		t.Errorf("Len = %d, want 1", b.Len())
	}
}

func TestCloseSubscription(t *testing.T) {
	var b Bus[string]
	s := b.Subscribe(1)
	s.Close()
	s.Close() // second close is harmless
	b.Publish("x")
	if _, open := <-s.C(); open {
		t.Error("channel still open after Close")
	}
	if s.Lagged() {
		t.Error("a normal Close reported as lagged")
	}
}

func TestCloseBus(t *testing.T) {
	var b Bus[int]
	s := b.Subscribe(1)
	b.Close()
	b.Publish(1) // must not panic on closed channels
	if _, open := <-s.C(); open {
		t.Error("subscription open after bus Close")
	}
	late := b.Subscribe(1)
	if _, open := <-late.C(); open {
		t.Error("subscription to a closed bus should start closed")
	}
}

func TestConcurrentUse(t *testing.T) {
	var b Bus[int]
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := range 1000 {
				b.Publish(i)
			}
		}()
		go func() {
			defer wg.Done()
			for range 50 {
				s := b.Subscribe(8)
				s.Close()
			}
		}()
	}
	wg.Wait()
}
