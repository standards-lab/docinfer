package docinfer

import (
	"context"
	"sync"
)

// claims is a [Pipeline]'s claim table: the join of work in flight across every job it runs. A
// key one caller has claimed and not yet released is answered once: every other caller that
// claims it meanwhile waits for the claimant's answer rather than asking the model again. It is
// one mechanism for a single key, an image's, a window's, or an extraction's, and for an
// embedding call's batch of keys. It is safe for concurrent use.
type claims struct {
	mu    sync.Mutex
	calls map[string]*claim
	// onWait, when set, is called as a waiter starts to wait: a test's hook, set before any job
	// runs, so a test can hold a leader until its waiters are waiting, without a sleep.
	onWait func()
}

// claim is one key's work in flight: done closes once its claimant has released it, its answer
// set before.
type claim struct {
	done   chan struct{}
	answer answer
}

// answer is what a claimant releases a key with: its result, or a final error. The zero answer is
// none, the claimant having ended without a result (cancelled, failed transiently or on the
// failed row, or panicking), and its waiters claim the key again.
type answer struct {
	// held is the key's result, put in the cache before the key is released, so a waiter's result
	// is what a cache read would give: a refusal is a result too.
	held *Held
	// err is a failure no other caller's request would change, a permanent one, which its waiters
	// fail with.
	err error
}

// waiting is a key another caller has claimed: its place among the keys claimed, and its claim.
type waiting struct {
	i int
	c *claim
}

// claim claims each of keys at once. A key no caller holds becomes the caller's, in the returned
// lease, and its index is in own; a key another caller holds, or one repeated in keys, is waited
// for, in waits. The caller answers or releases every key of the lease, and waits for the rest.
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

// wait waits for c's claimant to release it and returns its answer, or ctx's error, at once, when
// ctx ends first: a cancelled waiter doesn't wait out the claimant's request. A waiter holds no
// [Bounds] slot.
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

// lease is the keys one caller has claimed and not yet released. It is used by one goroutine at a
// time: the one that asks for its keys' results, or that hands them on by take.
type lease struct {
	t     *claims
	calls map[string]*claim
}

// take moves keys from l to a lease of their own, for another goroutine to answer.
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

// answer releases key with a, waking its waiters. The key is deleted from the table before they
// wake, so a waiter that claims it again, having had no answer, finds it free. A key l doesn't
// hold, already released, is left alone.
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

// release releases every key l still holds with no answer, so their waiters claim them again.
// A claimant defers it, so one that returns early, or panics, strands no waiter.
func (l *lease) release() {
	for k := range l.calls {
		l.answer(k, answer{})
	}
}
