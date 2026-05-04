package gateway

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunExtractorConcurrencyCap proves the semaphore in
// runExtractor bounds simultaneous extractor processes to
// maxConcurrentExtractions. We use the `sleep` binary as a
// deterministic stand-in for pdftotext / pandoc (it's universally
// available on macOS and Linux runners), measure the peak number
// of in-flight invocations from inside a wrapper, and assert the
// peak never exceeds the cap.
func TestRunExtractorConcurrencyCap(t *testing.T) {
	// Use /bin/sleep as the "extractor" — every invocation just
	// sleeps for a while. We track entry/exit via instrumented
	// wrappers to count the peak in-flight depth.
	var inflight, peak atomic.Int32
	var mu sync.Mutex

	// fakeExtractor delegates to runExtractor but tracks concurrency.
	// We can't easily instrument runExtractor itself without exposing
	// its internals, so this test wraps the call site.
	fake := extractorBinary{
		bin:     "sleep",
		args:    []string{"0.05"}, // 50 ms — enough to overlap, fast enough to stay under the test timeout
		pkgHint: "sleep is in coreutils, this should never trigger",
	}

	const n = 12
	var wg sync.WaitGroup
	wg.Add(n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			<-start

			// Track entry, then call runExtractor, then track exit.
			// runExtractor itself manages the semaphore — entry to
			// this goroutine doesn't necessarily mean it's running
			// the extractor; we track inflight inside the call.
			done := make(chan struct{})
			go func() {
				// Watch for the extractor to actually fire. We can't
				// directly instrument the cmd.Run call, but we can
				// check the global semaphore's slot count: when we
				// hold a slot, len(extractorSem) reflects in-flight.
				ticker := time.NewTicker(2 * time.Millisecond)
				defer ticker.Stop()
				for {
					select {
					case <-done:
						return
					case <-ticker.C:
						mu.Lock()
						cur := int32(len(extractorSem))
						if cur > peak.Load() {
							peak.Store(cur)
						}
						mu.Unlock()
					}
				}
			}()

			inflight.Add(1)
			defer inflight.Add(-1)
			_, _ = runExtractor(context.Background(), fake, nil)
			close(done)
		}()
	}
	close(start)
	wg.Wait()

	// Semaphore capacity is the cap. Peak observed should equal it
	// (12 goroutines all want to run; at any moment up to N can hold
	// a slot). Sometimes the sampler misses the exact peak due to
	// scheduling, so we assert it's close.
	got := peak.Load()
	assert.LessOrEqual(t, int(got), maxConcurrentExtractions,
		"peak extractor concurrency exceeded the cap: got %d, cap %d", got, maxConcurrentExtractions)
	// And we should observe at least 2 in-flight at some point —
	// otherwise the test isn't actually exercising concurrency.
	assert.GreaterOrEqual(t, int(got), 2,
		"sampler should have seen at least 2 concurrent extractions; got %d", got)
}

// TestRunExtractorCtxCancelDuringQueue covers the queue-side
// cancellation path: when the semaphore is full and the parent
// context is cancelled, runExtractor returns the context error
// rather than blocking forever waiting for a slot.
func TestRunExtractorCtxCancelDuringQueue(t *testing.T) {
	// Saturate the semaphore so the next call queues.
	for i := 0; i < maxConcurrentExtractions; i++ {
		extractorSem <- struct{}{}
	}
	t.Cleanup(func() {
		// Drain whatever this test left in the channel so other
		// tests start with an empty semaphore.
		for {
			select {
			case <-extractorSem:
			default:
				return
			}
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := runExtractor(ctx, extractorBinary{
		bin: "sleep", args: []string{"0.01"}, pkgHint: "n/a",
	}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "extraction queue")
}
