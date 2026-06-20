/*
 * Copyright (c) 2026 HSTLES / ORBTR Pty Ltd. All Rights Reserved.
 * Queries: licensing@hstles.com
 */

// Package repair holds the background sweeper that re-replicates
// under-replicated blobs.
//
// The sweeper itself is internal; the re-replication action (calling
// replicate.Replicator with a per-blob target) is supplied by the
// consumer, since only the consumer knows its own replication
// topology and rate budget.
package repair

import (
	"container/heap"
	"sync"
	"time"
)

// Item is a single blob queued for repair.
type Item struct {
	RootCID        string
	ReplicaCount   int
	LastVerifiedAt time.Time

	// index is heap-internal; ignore.
	index int
}

// priority sorts (replicaCount ASC, lastVerifiedAt ASC). Lower replica
// count repaired first; among equals the staler ones repaired first.
type queue []*Item

func (q queue) Len() int      { return len(q) }
func (q queue) Swap(i, j int) { q[i], q[j] = q[j], q[i]; q[i].index = i; q[j].index = j }
func (q queue) Less(i, j int) bool {
	if q[i].ReplicaCount != q[j].ReplicaCount {
		return q[i].ReplicaCount < q[j].ReplicaCount
	}
	return q[i].LastVerifiedAt.Before(q[j].LastVerifiedAt)
}
func (q *queue) Push(x any) {
	it := x.(*Item)
	it.index = len(*q)
	*q = append(*q, it)
}
func (q *queue) Pop() any {
	old := *q
	n := len(old)
	it := old[n-1]
	old[n-1] = nil
	it.index = -1
	*q = old[:n-1]
	return it
}

// Queue is the concurrency-safe priority queue surface.
type Queue struct {
	mu sync.Mutex
	q  queue
}

// NewQueue constructs an empty queue.
func NewQueue() *Queue { return &Queue{q: queue{}} }

// Push enqueues an item (idempotent on RootCID — duplicates replace).
func (Q *Queue) Push(it *Item) {
	if it == nil || it.RootCID == "" {
		return
	}
	Q.mu.Lock()
	defer Q.mu.Unlock()
	for i, existing := range Q.q {
		if existing.RootCID == it.RootCID {
			Q.q[i] = it
			heap.Fix(&Q.q, i)
			return
		}
	}
	heap.Push(&Q.q, it)
}

// Pop returns the highest-priority item, or nil when empty.
func (Q *Queue) Pop() *Item {
	Q.mu.Lock()
	defer Q.mu.Unlock()
	if Q.q.Len() == 0 {
		return nil
	}
	it := heap.Pop(&Q.q).(*Item)
	return it
}

// Len returns the current queue depth (useful for metrics).
func (Q *Queue) Len() int {
	Q.mu.Lock()
	defer Q.mu.Unlock()
	return Q.q.Len()
}
