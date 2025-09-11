package functional

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/deckhouse/deckhouse/pkg/log"
	"github.com/stretchr/testify/require"

	sh_task "github.com/flant/shell-operator/pkg/task"

	"github.com/flant/addon-operator/pkg/module_manager/scheduler/extenders"
)

// mockQueueService records AddLastTaskToQueue calls.
type mockQueueService struct {
	mu    sync.Mutex
	calls []string
}

// TestScheduler_MissingDependencyLaterAdded ensures that a request blocked by a
// missing dependency becomes schedulable once the dependency is later added and completed.
func TestScheduler_MissingDependencyLaterAdded(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mqs := &mockQueueService{}
	s := NewScheduler(ctx, mqs, log.NewNop())

	// D waits for X which is unknown at the moment.
	d := &Request{Name: "D", Dependencies: []extenders.Hint{{Name: "X"}}}
	s.Add(d)
	s.Done(Root)

	require.Eventually(t, func() bool { return mqs.len() == 0 }, 200*time.Millisecond, 20*time.Millisecond)

	// Now introduce X and kick scheduler again.
	x := &Request{Name: "X"}
	s.Add(x)
	s.Done(Root)

	// X should be scheduled first.
	require.Eventually(t, func() bool { return mqs.len() == 1 }, time.Second, 10*time.Millisecond)

	// Complete X; D should follow.
	s.Done("X")
	require.Eventually(t, func() bool { return mqs.len() == 2 }, time.Second, 10*time.Millisecond)

	s.Done("D")
	require.Eventually(t, s.Finished, time.Second, 10*time.Millisecond)
}

// TestScheduler_DuplicateAddIgnored confirms that adding the same request twice
// results in a single queue entry.
func TestScheduler_DuplicateAddIgnored(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mqs := &mockQueueService{}
	s := NewScheduler(ctx, mqs, log.NewNop())

	r := &Request{Name: "uniq"}
	s.Add(r)
	s.Add(r) // duplicate

	s.Done(Root)

	require.Eventually(t, func() bool { return mqs.len() == 1 }, time.Second, 10*time.Millisecond)
	s.Done("uniq")
	require.Eventually(t, s.Finished, time.Second, 10*time.Millisecond)
}

func (m *mockQueueService) AddLastTaskToQueue(_ string, task sh_task.Task) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls = append(m.calls, task.GetDescription())
	return nil
}

func (m *mockQueueService) len() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.calls)
}

func TestScheduler_LinearDependencies(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mqs := &mockQueueService{}
	s := NewScheduler(ctx, mqs, log.NewLogger(log.WithLevel(slog.LevelDebug)))

	// A → B
	a := &Request{Name: "A"}
	b := &Request{Name: "B", Dependencies: []extenders.Hint{{Name: "A"}}}

	s.Add(a, b)

	// trigger first scheduling.
	s.Done(Root)

	// wait for A to be sent to queue.
	require.Eventually(t, func() bool { return mqs.len() == 1 }, time.Second, 10*time.Millisecond)

	// mark A done – expect B scheduled.
	s.Done("A")
	require.Eventually(t, func() bool { return mqs.len() == 2 }, time.Second, 10*time.Millisecond)

	// mark B done - expect nothing scheduled
	s.Done("B")
	require.Eventually(t, func() bool { return mqs.len() == 2 }, time.Second, 10*time.Millisecond)

	time.Sleep(time.Second)

	require.True(t, s.Finished())
}

func TestScheduler_MultipleDependencies(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mqs := &mockQueueService{}
	s := NewScheduler(ctx, mqs, log.NewLogger(log.WithLevel(slog.LevelDebug)))

	// A, B → C
	a := &Request{Name: "A"}
	b := &Request{Name: "B"}
	c := &Request{Name: "C", Dependencies: []extenders.Hint{{Name: "A"}, {Name: "B"}}}

	s.Add(a, b, c)
	s.Done(Root)

	// A and B expected to be scheduled
	require.Eventually(t, func() bool { return mqs.len() == 2 }, time.Second, 10*time.Millisecond)

	// mark A done - expect nothing scheduled
	s.Done("A")
	require.Never(t, func() bool { return mqs.len() == 3 }, 200*time.Millisecond, 20*time.Millisecond, "C must wait for B")

	// mark B done, both deps done so expect C scheduled
	s.Done("B")
	require.Eventually(t, func() bool { return mqs.len() == 3 }, time.Second, 10*time.Millisecond)

	// mark C done - expect nothing scheduled
	s.Done("C")
	require.Eventually(t, func() bool { return mqs.len() == 3 }, time.Second, 10*time.Millisecond)

	time.Sleep(time.Second)

	require.True(t, s.Finished())
}

func TestScheduler_MissingDependencyBlocks(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	mqs := &mockQueueService{}
	s := NewScheduler(ctx, mqs, log.NewNop())

	// D depends on never-done X
	d := &Request{Name: "D", Dependencies: []extenders.Hint{{Name: "X"}}}
	s.Add(d)
	s.Done(Root)

	// scheduler should not call queue.
	require.Eventually(t, func() bool { return mqs.len() == 0 }, 300*time.Millisecond, 20*time.Millisecond)

	require.False(t, s.Finished())
}

func TestScheduler_RemoveClearsDone(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mqs := &mockQueueService{}
	s := NewScheduler(ctx, mqs, log.NewLogger(log.WithLevel(slog.LevelDebug)))

	// build a chain: operator-prometheus → prometheus → monitoring-application
	a := &Request{Name: "operator-prometheus"}
	b := &Request{Name: "prometheus", Dependencies: []extenders.Hint{{Name: "operator-prometheus"}}}
	c := &Request{Name: "monitoring-application", Dependencies: []extenders.Hint{{Name: "prometheus"}}}

	s.Add(a, b, c)
	s.Done(Root)

	// A should be the only task queued.
	require.Eventually(t, func() bool { return mqs.len() == 1 }, time.Second, 10*time.Millisecond)

	// mark operator-prometheus done → expect prometheus scheduled.
	s.Done("operator-prometheus")
	require.Eventually(t, func() bool { return mqs.len() == 2 }, time.Second, 10*time.Millisecond)

	// mark prometheus done → expect monitoring-application scheduled.
	s.Done("prometheus")
	require.Eventually(t, func() bool { return mqs.len() == 3 }, time.Second, 10*time.Millisecond)

	// mark monitoring-application done - expect nothing scheduled
	s.Done("monitoring-application")
	require.Eventually(t, func() bool { return mqs.len() == 3 }, time.Second, 10*time.Millisecond)

	// scheduler reports finished.
	require.True(t, s.Finished())

	s.Remove("operator-prometheus")

	// trigger prometheus done
	s.Done("prometheus")

	// expect NO additional tasks to be scheduled because operator-prometheus removed.
	require.Never(t, func() bool { return mqs.len() > 3 }, 300*time.Millisecond, 20*time.Millisecond, "No new tasks must be scheduled when the dependency chain is broken")
}
