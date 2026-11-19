package docinfer

import (
	"context"
	"fmt"
)

// Bounds bounds the requests in flight per capability, across every [Pipeline] built with it. A
// host holds one Bounds per replica and builds each profile's pipeline over it, so it bounds each
// capability as a whole whatever profiles its jobs use. It is safe for concurrent use, and its
// zero value is not ready; [NewBounds] makes one.
type Bounds struct {
	// slots holds one semaphore per capability: a request in flight holds a slot, a buffered
	// channel's place, from before its call until after it returns.
	slots map[Capability]chan struct{}
	// onWait, when set, is called as a request finds its capability's slots all held and starts to
	// wait: a test's hook, set before any request, so a test sees requests held at a bound without
	// a sleep.
	onWait func(Capability)
}

// NewBounds returns a Bounds of at most vision, transcription, extraction, and embeddings requests
// in flight for each capability. Each has no default: a bound below 1 is refused, matching
// [ErrRequest].
func NewBounds(vision, transcription, extraction, embeddings int) (*Bounds, error) {
	bounds := []struct {
		capability Capability
		n          int
	}{
		{CapabilityVision, vision},
		{CapabilityTranscription, transcription},
		{CapabilityExtraction, extraction},
		{CapabilityEmbeddings, embeddings},
	}
	b := &Bounds{slots: make(map[Capability]chan struct{}, len(bounds))}
	for _, c := range bounds {
		if c.n < 1 {
			return nil, fmt.Errorf("docinfer: NewBounds: %w: %s bound %d is below 1", ErrRequest, c.capability, c.n)
		}
		b.slots[c.capability] = make(chan struct{}, c.n)
	}
	return b, nil
}

// do runs call, one request of capability c, once a slot of c's bound is free, and frees the slot
// when call returns, a panic included. It waits for a slot only as long as ctx lasts, returning
// ctx's error unwrapped, without calling call, when ctx ends first. It is the one runner every
// capability's model request goes through, so each is bounded the same way.
func (b *Bounds) do(ctx context.Context, c Capability, call func(ctx context.Context) error) error {
	slots := b.slots[c]
	if slots == nil {
		panic(fmt.Sprintf("docinfer: no bound for capability %q", c)) // a capability is a constant
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case slots <- struct{}{}:
	default:
		if b.onWait != nil {
			b.onWait(c)
		}
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	defer func() { <-slots }()
	return call(ctx)
}
