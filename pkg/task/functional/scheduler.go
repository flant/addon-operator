package functional

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/deckhouse/deckhouse/pkg/log"
	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/task"
	"github.com/flant/addon-operator/pkg/task/queue"
	sh_task "github.com/flant/shell-operator/pkg/task"
)

const (
	channelsBuffer = 24
)

type Scheduler struct {
	queueService *queue.Service
	logger       *log.Logger

	// batch control
	cancel context.CancelFunc

	// for safe shutdown on replacement
	wg *sync.WaitGroup

	mtx       sync.Mutex
	done      map[string]struct{}
	scheduled map[string]struct{}

	doneCh    chan string
	processCh chan *Request
}

type Request struct {
	Name         string
	Description  string
	Dependencies []string
	IsReloadAll  bool
	DoStartup    bool
	Labels       map[string]string
}

func NewScheduler(qService *queue.Service, logger *log.Logger) *Scheduler {
	return &Scheduler{
		queueService: qService,
		logger:       logger,
		wg:           new(sync.WaitGroup),
	}
}

// Start schedules a new batch, canceling the previous one if active.
func (s *Scheduler) Start(ctx context.Context, modules []*Request) {
	// cancel the previous batch.
	if s.cancel != nil {
		s.cancel()
		// wait for batch goroutines to finish
		s.wg.Wait()
	}

	s.logger.Debug("following functional modules will be scheduled", slog.Any("modules", modules))

	// initialize new batch state.
	batchCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel

	s.mtx.Lock()
	s.done = make(map[string]struct{}, len(modules))
	s.scheduled = make(map[string]struct{}, len(modules))
	s.mtx.Unlock()

	s.doneCh = make(chan string, channelsBuffer)
	s.processCh = make(chan *Request, channelsBuffer)

	s.wg.Add(2)
	go func() {
		defer s.wg.Done()
		s.runScheduleLoop(batchCtx, modules)
	}()

	go func() {
		defer s.wg.Done()
		s.runProcessLoop(batchCtx)
	}()
}

// runScheduleLoop launches the scheduling loop for a batch.
func (s *Scheduler) runScheduleLoop(ctx context.Context, modules []*Request) {
	s.reschedule("", modules)

	for {
		select {
		case <-ctx.Done():
			return
		case name, ok := <-s.doneCh:
			if !ok {
				return
			}
			if s.reschedule(name, modules) {
				return
			}
		}
	}
}

// process waits for requests to be processed
func (s *Scheduler) runProcessLoop(ctx context.Context) {
	var idx int
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-s.processCh:
			s.handleRequest(idx, req)
			idx++
		}
	}
}

// reschedule marks module done and schedule new modules to be processed
func (s *Scheduler) reschedule(name string, modules []*Request) bool {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	// skip not present in the batch modules
	if _, ok := s.scheduled[name]; !ok && name != "" {
		return false
	}

	// mark module done
	s.done[name] = struct{}{}

	for _, req := range modules {
		// skip already processed
		if _, ok := s.done[req.Name]; ok {
			continue
		}

		// skip already scheduled
		if _, ok := s.scheduled[req.Name]; ok {
			continue
		}

		// check if all dependencies done
		ready := true
		for _, dep := range req.Dependencies {
			if _, ok := s.done[dep]; !ok {
				ready = false
				break
			}
		}

		// schedule module if ready
		if ready {
			s.logger.Debug("the '%s' module scheduling triggered by '%s'", req.Name, name)
			s.scheduled[req.Name] = struct{}{}
			s.processCh <- req
		}
	}

	// check if all modules processed
	return len(s.done) == len(modules)
}

// handleRequest creates a ModuleRun task for request in a parallel queue
func (s *Scheduler) handleRequest(idx int, req *Request) {
	queueName := fmt.Sprintf(app.ParallelQueueNamePattern, idx%(app.NumberOfParallelQueues-1))

	newTask := sh_task.NewTask(task.ModuleRun).
		WithLogLabels(req.Labels).
		WithQueueName(queueName).
		WithMetadata(task.HookMetadata{
			EventDescription: req.Description,
			ModuleName:       req.Name,
			DoModuleStartup:  req.DoStartup,
			IsReloadAll:      req.IsReloadAll,
		})

	_ = s.queueService.AddLastTaskToQueue(queueName, newTask)
}

// Done sends signal that module processing done
func (s *Scheduler) Done(name string) {
	s.doneCh <- name
}

// Stop is the graceful shutdown
func (s *Scheduler) Stop() {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if s.cancel != nil {
		s.cancel()
		s.wg.Wait()

		close(s.doneCh)
		close(s.processCh)
	}
}
