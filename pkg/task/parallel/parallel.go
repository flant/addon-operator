package parallel

import "sync"

type queueEvent struct {
	moduleName string
	errMsg     string
	succeeded  bool
}

func (e queueEvent) ModuleName() string {
	return e.moduleName
}

func (e queueEvent) ErrorMessage() string {
	return e.errMsg
}

func (e queueEvent) Succeeded() bool {
	return e.succeeded
}

type TaskChannel chan queueEvent

func NewTaskChannel() TaskChannel {
	return TaskChannel(make(chan queueEvent))
}

func (t TaskChannel) SendSuccess(moduleName string) {
	t <- queueEvent{
		moduleName: moduleName,
		succeeded:  true,
	}
}

func (t TaskChannel) SendFailure(moduleName string, errMsg string) {
	t <- queueEvent{
		moduleName: moduleName,
		errMsg:     errMsg,
		succeeded:  false,
	}
}

type TaskChannels struct {
	l        sync.Mutex
	channels map[string]TaskChannel
}

func NewTaskChannels() *TaskChannels {
	return &TaskChannels{
		channels: make(map[string]TaskChannel),
	}
}

func (pq *TaskChannels) Channels() []string {
	pq.l.Lock()
	defer pq.l.Unlock()

	var ids []string

	for id := range pq.channels {
		ids = append(ids, id)
	}

	return ids
}

func (pq *TaskChannels) Set(id string, c TaskChannel) {
	pq.l.Lock()
	pq.channels[id] = c
	pq.l.Unlock()
}

func (pq *TaskChannels) Get(id string) (TaskChannel, bool) {
	pq.l.Lock()
	defer pq.l.Unlock()
	c, ok := pq.channels[id]
	return c, ok
}

func (pq *TaskChannels) Delete(id string) {
	pq.l.Lock()
	delete(pq.channels, id)
	pq.l.Unlock()
}
