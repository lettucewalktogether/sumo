package cron

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchedulerAddAndRun(t *testing.T) {
	var callCount atomic.Int32

	s := NewScheduler()
	err := s.Add(Job{
		Name:     "test-job",
		Schedule: "50ms",
		Prompt:   "do something",
		AgentFn: func(ctx context.Context, prompt string) (string, error) {
			callCount.Add(1)
			assert.Equal(t, "do something", prompt)
			return "done", nil
		},
	})
	require.NoError(t, err)

	assert.Len(t, s.Jobs(), 1)

	s.Start(context.Background())
	time.Sleep(200 * time.Millisecond)
	s.Stop()

	assert.GreaterOrEqual(t, callCount.Load(), int32(1))
}

// TestSchedulerRunNow exercises the off-cycle "run now" path used by
// the chat UI's Jobs tab. Critical behaviour: AgentFn fires
// immediately (well before the next scheduled tick), and the
// scheduler reports a clean error for an unknown job rather than
// silently no-op'ing.
func TestSchedulerRunNow(t *testing.T) {
	t.Run("fires_off_cycle", func(t *testing.T) {
		var calls atomic.Int32
		s := NewScheduler()
		require.NoError(t, s.Add(Job{
			Name:     "manual-trigger",
			Schedule: "1h", // wouldn't tick during the test window
			Prompt:   "hello",
			AgentFn: func(ctx context.Context, prompt string) (string, error) {
				calls.Add(1)
				return "ok", nil
			},
		}))
		s.Start(context.Background())
		defer s.Stop()
		require.NoError(t, s.RunNow("manual-trigger"))
		// Allow the goroutine to actually run.
		require.Eventually(t, func() bool { return calls.Load() >= 1 },
			500*time.Millisecond, 10*time.Millisecond,
			"AgentFn should have been called once via RunNow")
	})

	t.Run("unknown_job_errors", func(t *testing.T) {
		s := NewScheduler()
		s.Start(context.Background())
		defer s.Stop()
		err := s.RunNow("ghost")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("scheduler_not_started_errors", func(t *testing.T) {
		s := NewScheduler()
		require.NoError(t, s.Add(Job{
			Name: "x", Schedule: "1h", Prompt: "p",
			AgentFn: func(ctx context.Context, prompt string) (string, error) { return "", nil },
		}))
		err := s.RunNow("x")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not started")
	})
}

func TestSchedulerInvalidSchedule(t *testing.T) {
	s := NewScheduler()
	err := s.Add(Job{
		Name:     "bad-job",
		Schedule: "invalid",
		Prompt:   "test",
		AgentFn: func(ctx context.Context, prompt string) (string, error) {
			return "", nil
		},
	})
	assert.Error(t, err)
}

func TestSchedulerStop(t *testing.T) {
	s := NewScheduler()
	s.Add(Job{
		Name:     "slow-job",
		Schedule: "1h",
		Prompt:   "test",
		AgentFn: func(ctx context.Context, prompt string) (string, error) {
			return "ok", nil
		},
	})

	s.Start(context.Background())

	done := make(chan struct{})
	go func() {
		s.Stop()
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() did not return in time")
	}
}

func TestSchedulerMultipleJobs(t *testing.T) {
	var count1, count2 atomic.Int32

	s := NewScheduler()
	s.Add(Job{
		Name:     "job1",
		Schedule: "50ms",
		Prompt:   "first",
		AgentFn: func(ctx context.Context, prompt string) (string, error) {
			count1.Add(1)
			return "ok", nil
		},
	})
	s.Add(Job{
		Name:     "job2",
		Schedule: "50ms",
		Prompt:   "second",
		AgentFn: func(ctx context.Context, prompt string) (string, error) {
			count2.Add(1)
			return "ok", nil
		},
	})

	assert.Len(t, s.Jobs(), 2)

	s.Start(context.Background())
	time.Sleep(200 * time.Millisecond)
	s.Stop()

	assert.GreaterOrEqual(t, count1.Load(), int32(1))
	assert.GreaterOrEqual(t, count2.Load(), int32(1))
}
