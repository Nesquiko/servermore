package runner

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Nesquiko/servermore/pkg/assert"
	"github.com/Nesquiko/servermore/pkg/server"
	"github.com/google/uuid"
)

const (
	InvocationsQueueMaxLen = 100
)

// when starting instance:
// - is there already an instance associated with funcPath
//   - if yes return it the channel with the UUID
//   - if starting return the channel
//   - if no return MultiConsumer
type InstancesStates struct {
	funcPathToId   map[server.AbsolutePath]uuid.UUID
	funcPathToIdMu sync.RWMutex

	startingInstances map[server.AbsolutePath]*MultiConsumer[uuid.UUID]
	startingMu        sync.Mutex

	instanceStates   map[uuid.UUID]*instanceState
	instanceStatesMu sync.RWMutex
}

type instanceState struct {
	id       uuid.UUID
	funcPath server.AbsolutePath

	runtime FunctionRuntime

	queue  chan invocation
	opened atomic.Bool

	lastUsedTimer         *time.Timer
	instanceShutdownAfter time.Duration

	workerCtx    context.Context
	workerCancel context.CancelFunc
}

func (is *instanceState) ResetTimer() {
	resetted := is.lastUsedTimer.Reset(is.instanceShutdownAfter)
	assert.That(resetted, "calling ResetTimer on already triggered timer")
}

func (is *instanceState) AddToQueue(req InvocationRequest) <-chan *InvocationResult {
	assert.That(is.opened.Load(), "adding request to already closed queue")

	resCh := make(chan *InvocationResult)
	is.ResetTimer()
	is.queue <- invocation{req: req, resCh: resCh}

	return resCh
}

type invocation struct {
	req   InvocationRequest
	resCh chan *InvocationResult
}

type InvocationRequest struct {
	Method  string
	Path    string
	Headers map[string]string
	Body    []byte
}

type InvocationResult struct {
	resp *InvokeInstanceResponse
	err  error
}

func NewInstanceStates() *InstancesStates {
	return &InstancesStates{
		funcPathToId:      map[server.AbsolutePath]uuid.UUID{},
		funcPathToIdMu:    sync.RWMutex{},
		startingInstances: map[server.AbsolutePath]*MultiConsumer[uuid.UUID]{},
		startingMu:        sync.Mutex{},
		instanceStates:    map[uuid.UUID]*instanceState{},
		instanceStatesMu:  sync.RWMutex{},
	}
}

// IsAssignedOrStartIt checks if the given funcPath has a running instance,
// if yes, returns its id in channel. If it is only starting, returns the channel
// through which the instance id will be returned.
// If no, it is the callers responsibility to start the instance and submit its
// id in this order:
//  1. start the instance
//  2. submits it id to InstancesStates (using [AssignId])
//  3. answers subscribers by submitting the result to the returned MultiConsumer
func (m *InstancesStates) IsAssignedOrStartIt(
	funcPath server.AbsolutePath,
) (bool, *MultiConsumer[uuid.UUID], <-chan uuid.UUID) {
	m.funcPathToIdMu.RLock()
	if instanceId, ok := m.funcPathToId[funcPath]; ok {
		resCh := make(chan uuid.UUID, 1)
		resCh <- instanceId
		m.funcPathToIdMu.RUnlock()
		return true, nil, resCh
	}
	m.funcPathToIdMu.RUnlock()

	m.startingMu.Lock()
	defer m.startingMu.Unlock()
	if consumer, ok := m.startingInstances[funcPath]; ok {
		return true, nil, consumer.AddSub()
	}

	resultConsumers := NewMultiConsumer[uuid.UUID]()
	m.startingInstances[funcPath] = resultConsumers
	return false, resultConsumers, nil
}

func (m *InstancesStates) Submit(
	funcPath server.AbsolutePath,
	instanceId uuid.UUID,
	shutdownAfter time.Duration,
	runtime FunctionRuntime,
) *instanceState {
	m.funcPathToIdMu.Lock()
	m.funcPathToId[funcPath] = instanceId
	m.funcPathToIdMu.Unlock()

	m.startingMu.Lock()
	delete(m.startingInstances, funcPath)
	m.startingMu.Unlock()

	workerCtx, workerCancelCtx := context.WithCancel(context.Background())
	m.instanceStatesMu.Lock()
	state := instanceState{
		id:                    instanceId,
		funcPath:              funcPath,
		runtime:               runtime,
		queue:                 make(chan invocation, InvocationsQueueMaxLen),
		opened:                atomic.Bool{},
		instanceShutdownAfter: shutdownAfter,
		lastUsedTimer:         time.NewTimer(shutdownAfter),
		workerCtx:             workerCtx,
		workerCancel:          workerCancelCtx,
	}
	m.instanceStates[instanceId] = &state
	m.instanceStatesMu.Unlock()
	return &state
}

// QueueDepths reads the lengths of queues WITHOUGH LOCKING to not slowdown
// the request processing, it is OK that it returns not precise data
func (m *InstancesStates) QueueDepths() map[string]uint32 {
	depths := make(map[string]uint32)
	for k, v := range m.instanceStates {
		depths[k.String()] = uint32(len(v.queue))
	}
	return depths
}

// Invoke returns boolean indicating if instance is running, or error, caller
// must handle different cases:
//   - err != nil => the instance wasn't found
//   - running == false => then the caller must become a "worker" and must use returned
//     instance state to send all requests in queue to the instance
//   - running == true => caller cann add request to the queue and will receive the response from the channel
func (m *InstancesStates) InstanceState(instanceId uuid.UUID) (bool, *instanceState, error) {
	m.instanceStatesMu.RLock()
	instance, ok := m.instanceStates[instanceId]
	m.instanceStatesMu.RUnlock()

	if !ok {
		return false, nil, fmt.Errorf("instance didn't start")
	}

	if instance.opened.CompareAndSwap(false, true) {
		return false, instance, nil
	}

	return true, nil, nil
}

func (is *InstancesStates) StopInstance(instance *instanceState) {
	instance.opened.Store(false)
	instance.workerCancel()
	close(instance.queue)
	instance.lastUsedTimer.Stop()

	is.funcPathToIdMu.Lock()
	delete(is.funcPathToId, instance.funcPath)
	is.funcPathToIdMu.Unlock()

	is.instanceStatesMu.Lock()
	delete(is.instanceStates, instance.id)
	is.instanceStatesMu.Unlock()
	instance.runtime.Stop()
}

type DownloadsSyncMap struct {
	downloads sync.Map
}

func NewDownloadsSyncMap() *DownloadsSyncMap {
	return &DownloadsSyncMap{downloads: sync.Map{}}
}

type DownloadResult struct {
	path server.AbsolutePath
	err  error
}

// IsDownloadedOrStartDownload check if path is being downloaded:
//   - if no, then it is the callers responsibility to download it and use broadcast for sending the result
//   - if yes, then return the result channel
func (m *DownloadsSyncMap) IsDownloadedOrStartDownload(
	path server.AbsolutePath,
) (bool, *MultiConsumer[DownloadResult], <-chan DownloadResult) {
	resultConsumers := NewMultiConsumer[DownloadResult]()
	value, isDownloading := m.downloads.LoadOrStore(path, resultConsumers)

	if !isDownloading {
		// if not downloading, then make it the callers responsibility to download it and send it
		return false, resultConsumers, nil
	}

	// somebody is downloading
	broadcast, ok := value.(*MultiConsumer[DownloadResult])
	if !ok {
		slog.Error(
			"DownloadsSyncMap contains value of unexpected type",
			"value", value,
		)
		return false, nil, nil
	}

	return true, nil, broadcast.AddSub()
}

func (m *DownloadsSyncMap) Delete(path server.AbsolutePath) {
	assert.That(path != "", "path can't be empty")
	m.downloads.Delete(path)
}

type MultiConsumer[T any] struct {
	subscribers []chan<- T
	mu          sync.Mutex
}

func NewMultiConsumer[T any]() *MultiConsumer[T] {
	return &MultiConsumer[T]{subscribers: make([]chan<- T, 0)}
}

func (b *MultiConsumer[T]) AddSub() <-chan T {
	newSub := make(chan T, 1)
	b.mu.Lock()
	b.subscribers = append(b.subscribers, newSub)
	b.mu.Unlock()
	return newSub
}

func (b *MultiConsumer[T]) SubmitResult(result T) {
	b.mu.Lock()
	for _, sub := range b.subscribers {
		sub <- result
		close(sub)
	}
	b.subscribers = nil
	b.mu.Unlock()
}
