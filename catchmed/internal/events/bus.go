// Package events fans out values to many subscribers without letting a slow
// one hold up the rest.
package events

import (
	"sync"
	"sync/atomic"
)

// Bus delivers every published value to every subscriber, in publish order.
// Publish never blocks: a subscriber that falls more than its buffer behind
// is dropped and its channel closed, and Lagged tells it so, so it can reload
// the full state instead of silently missing events. The zero Bus is ready
// to use.
type Bus[T any] struct {
	mu     sync.Mutex
	subs   map[*Subscription[T]]struct{}
	closed bool
}

// Subscription is one subscriber's view of a Bus.
type Subscription[T any] struct {
	c      chan T
	bus    *Bus[T]
	lagged atomic.Bool
}

// C returns the channel values arrive on. It is closed when the
// subscription ends: Close, the bus closing, or falling behind (see Lagged).
func (s *Subscription[T]) C() <-chan T { return s.c }

// Lagged reports whether the subscription was dropped for falling behind.
func (s *Subscription[T]) Lagged() bool { return s.lagged.Load() }

// Close ends the subscription. It is safe to call more than once.
func (s *Subscription[T]) Close() { s.bus.drop(s, false) }

// Subscribe registers a subscriber that can be up to buffer values behind.
func (b *Bus[T]) Subscribe(buffer int) *Subscription[T] {
	s := &Subscription[T]{c: make(chan T, max(buffer, 1)), bus: b}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		close(s.c)
		return s
	}
	if b.subs == nil {
		b.subs = make(map[*Subscription[T]]struct{})
	}
	b.subs[s] = struct{}{}
	return s
}

// Publish sends v to every subscriber without waiting for any of them.
func (b *Bus[T]) Publish(v T) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for s := range b.subs {
		select {
		case s.c <- v:
		default: // buffer full: this subscriber can no longer keep up
			s.lagged.Store(true)
			delete(b.subs, s)
			close(s.c)
		}
	}
}

// Close ends every subscription. Later Publish calls do nothing and later
// subscriptions start closed.
func (b *Bus[T]) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	for s := range b.subs {
		delete(b.subs, s)
		close(s.c)
	}
}

// Len returns the number of active subscribers.
func (b *Bus[T]) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.subs)
}

func (b *Bus[T]) drop(s *Subscription[T], lagged bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.subs[s]; ok {
		s.lagged.Store(lagged)
		delete(b.subs, s)
		close(s.c)
	}
}
