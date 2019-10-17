package task

import (
	"io"
	"os"

	log "github.com/sirupsen/logrus"
)

type TasksQueueDumper struct {
	DumpFilePath string
	queue        *TasksQueue
	eventCh      chan struct{}
}

func NewTasksQueueDumper(dumpFilePath string, queue *TasksQueue) *TasksQueueDumper {
	result := &TasksQueueDumper{
		DumpFilePath: dumpFilePath,
		queue:        queue,
		eventCh:      make(chan struct{}, 1),
	}
	go result.WatchQueue()
	return result
}

// QueueChangeCallback dumps a queue to a dump file when queue changes.
func (t *TasksQueueDumper) QueueChangeCallback() {
	t.eventCh <- struct{}{}
}

func (t *TasksQueueDumper) WatchQueue() {
	for {
		select {
		case <-t.eventCh:
			t.DumpQueue()
		}
	}
}

func (t *TasksQueueDumper) DumpQueue() {
	f, err := os.Create(t.DumpFilePath)
	if err != nil {
		log.Errorf("TasksQueueDumper: Cannot open '%s': %s\n", t.DumpFilePath, err)
	}
	_, err = io.Copy(f, t.queue.DumpReader())
	if err != nil {
		log.Errorf("TasksQueueDumper: Cannot dump tasks to '%s': %s\n", t.DumpFilePath, err)
	}
	err = f.Close()
	if err != nil {
		log.Errorf("TasksQueueDumper: Cannot close '%s': %s\n", t.DumpFilePath, err)
	}
}
