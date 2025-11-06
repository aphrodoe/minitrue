package query

import (
	"log"
	"sync"
)

// SyncTask represents a requested synchronization operation.
type SyncTask struct {
	PeerID     string
	DeviceID   string
	MetricName string
}

// SyncManager manages a queue of synchronization tasks and bounds concurrent execution.
type SyncManager struct {
	tasks    chan SyncTask
	inFlight map[string]bool
	mu       sync.Mutex
	syncFunc func(peerID, deviceID, metric string)
}

// NewSyncManager creates a new SyncManager.
func NewSyncManager(syncFunc func(peerID, deviceID, metric string)) *SyncManager {
	return &SyncManager{
		tasks:    make(chan SyncTask, 1000), // Buffer size for pending syncs
		inFlight: make(map[string]bool),
		syncFunc: syncFunc,
	}
}

// Start launches the specified number of worker goroutines.
func (sm *SyncManager) Start(workers int) {
	for i := 0; i < workers; i++ {
		go sm.worker()
	}
	log.Printf("[SyncManager] Started with %d concurrent workers", workers)
}

// Enqueue adds a sync task if it's not already in flight or in the queue.
func (sm *SyncManager) Enqueue(peerID, deviceID, metric string) {
	key := deviceID + "|" + metric

	sm.mu.Lock()
	if sm.inFlight[key] {
		sm.mu.Unlock()
		return
	}
	sm.inFlight[key] = true
	sm.mu.Unlock()

	select {
	case sm.tasks <- SyncTask{PeerID: peerID, DeviceID: deviceID, MetricName: metric}:
		// Successfully queued
	default:
		// Queue full, drop it and clear in-flight state so it can be retried later
		log.Printf("[SyncManager] Queue is full, dropping sync task for %s", key)
		sm.mu.Lock()
		delete(sm.inFlight, key)
		sm.mu.Unlock()
	}
}

// worker processes tasks from the queue.
func (sm *SyncManager) worker() {
	for task := range sm.tasks {
		// Execute the sync logic synchronously in this worker
		sm.syncFunc(task.PeerID, task.DeviceID, task.MetricName)

		// Remove from in-flight map so it can be queued again in the future if needed
		key := task.DeviceID + "|" + task.MetricName
		sm.mu.Lock()
		delete(sm.inFlight, key)
		sm.mu.Unlock()
	}
}
