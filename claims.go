package docinfer

import (
	"context"
	"sync"
)

// See docs/join-of-work-in-flight.md
type claims struct {
	mu     sync.Mutex
	calls  map[string]*claim
	onWait func()
}

type claim struct {
	done   chan struct{}
	answer answer
}

type answer struct {
	held *Held
	err  error
}

type waiting struct {
	i int
	c *claim
}

func (t *claims) claim(keys []string) (l *lease, own []int, waits []waiting) {
	l = &lease{t: t, calls: map[string]*claim{}}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.calls == nil {
		t.calls = map[string]*claim{}
	}
	for i, k := range keys {
		if c, ok := t.calls[k]; ok {
			waits = append(waits, waiting{i, c})
			continue
		}
		c := &claim{done: make(chan struct{})}
		t.calls[k] = c
		l.calls[k] = c
		own = append(own, i)
	}
	return l, own, waits
}

func (t *claims) wait(ctx context.Context, c *claim) (answer, error) {
	if t.onWait != nil {
		t.onWait()
	}
	select {
	case <-c.done:
		return c.answer, nil
	case <-ctx.Done():
		return answer{}, ctx.Err()
	}
}

type lease struct {
	t     *claims
	calls map[string]*claim
}

func (l *lease) take(keys ...string) *lease {
	sub := &lease{t: l.t, calls: make(map[string]*claim, len(keys))}
	for _, k := range keys {
		if c, ok := l.calls[k]; ok {
			sub.calls[k] = c
			delete(l.calls, k)
		}
	}
	return sub
}

func (l *lease) answer(key string, a answer) {
	c, ok := l.calls[key]
	if !ok {
		return
	}
	delete(l.calls, key)
	c.answer = a
	l.t.mu.Lock()
	delete(l.t.calls, key)
	l.t.mu.Unlock()
	close(c.done)
}

func (l *lease) release() {
	for k := range l.calls {
		l.answer(k, answer{})
	}
}
