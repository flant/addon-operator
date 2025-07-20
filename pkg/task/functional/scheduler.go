package functional

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/deckhouse/deckhouse/pkg/log"

	"github.com/flant/addon-operator/pkg/app"
	"github.com/flant/addon-operator/pkg/task"
	sh_task "github.com/flant/shell-operator/pkg/task"
)

const (
	// Root triggers modules without dependencies
	Root = ""
)

// Scheduler is used to process functional modules,
// it waits until modules` dependencies are processed and then
// runs ModuleRun tasks for them in parallel queues
type Scheduler struct {
	queueService queueService
	logger       *log.Logger

	mtx   sync.Mutex
	count int

	requests  map[string]*Request
	done      map[string]struct{}
	scheduled map[string]struct{}
}

type queueService interface {
	AddLastTaskToQueue(queueName string, task sh_task.Task) error
}

// Request describes a module and run task options
type Request struct {
	Name         string
	Description  string
	Dependencies []string
	IsReloadAll  bool
	DoStartup    bool
	Labels       map[string]string
}

// NewScheduler creates a scheduler instance and starts it
func NewScheduler(qService queueService, logger *log.Logger) *Scheduler {
	return &Scheduler{
		queueService: qService,
		logger:       logger,
		scheduled:    make(map[string]struct{}),
		requests:     make(map[string]*Request),
		done:         make(map[string]struct{}),
	}
}

// Add adds requests to process
func (s *Scheduler) Add(reqs ...*Request) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	for _, req := range reqs {
		s.logger.Debug("add request", slog.Any("request", req))
		// update module
		s.requests[req.Name] = req
		// undone module
		delete(s.done, req.Name)
		// unschedule module
		delete(s.scheduled, req.Name)
	}
}

// Remove removes module from done
// TODO(ipaqsa): stop module run task(now it is done by converge task)
func (s *Scheduler) Remove(name string) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	// undone module
	delete(s.done, name)
	// unschedule module
	delete(s.scheduled, name)
	// remove module
	delete(s.requests, name)
}

// reschedule marks module done and schedule new modules to be processed
func (s *Scheduler) reschedule(done string) {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	if done != Root {
		// skip unscheduled
		if _, ok := s.scheduled[done]; !ok {
			return
		}

		// mark done
		s.done[done] = struct{}{}

		// delete from scheduled
		delete(s.scheduled, done)
	}

	for _, req := range s.requests {
		// skip processed
		if _, ok := s.done[req.Name]; ok {
			continue
		}

		// skip scheduled
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
			s.logger.Debug("trigger scheduling", slog.String("scheduled", req.Name), slog.Any("done", done))
			s.scheduled[req.Name] = struct{}{}
			s.handleRequest(req)
		}
	}
}

// handleRequest creates a ModuleRun task for request in a parallel queue
func (s *Scheduler) handleRequest(req *Request) {
	queueName := fmt.Sprintf(app.ParallelQueueNamePattern, s.count%(app.NumberOfParallelQueues-1))

	moduleTask := sh_task.NewTask(task.ModuleRun).
		WithLogLabels(req.Labels).
		WithQueueName(queueName).
		WithMetadata(task.HookMetadata{
			EventDescription: req.Description,
			ModuleName:       req.Name,
			DoModuleStartup:  req.DoStartup,
			IsReloadAll:      req.IsReloadAll,
		})

	if err := s.queueService.AddLastTaskToQueue(queueName, moduleTask); err != nil {
		s.logger.Error("add last task to queue", slog.String("queue", queueName), slog.Any("error", err))
	}

	s.count++
}

// Done sends signal that module processing done
func (s *Scheduler) Done(name string) {
	s.reschedule(name)
}

// Finished defines if processing done
func (s *Scheduler) Finished() bool {
	s.mtx.Lock()
	defer s.mtx.Unlock()

	return len(s.done) == len(s.requests)
}
