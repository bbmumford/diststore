/*
 * Copyright (c) 2026 Brandon Miller-Mumford.
 * Licensed under the MIT License; see LICENSE.
 */

// Package quota implements the headroom-based disk budget that gates
// new writes against the cache.
//
// The budget is derived from a DiskState snapshot (total / free /
// our-storage) plus four tunables — MaxPct (ceiling we will ever
// occupy), TargetHeadroomPct (fraction of the remaining headroom we
// claim per refresh), MinReserveGB (hard floor of free disk we leave
// for the host) and HardMinGB (below this we refuse all writes).
// Manager polls the DiskStatter on a configurable interval and serves
// the current answer via CanStore / HasRoomFor / CurrentBudget.
package quota

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// DiskState is the headroom-based quota input. Values are in bytes
// except where a unit is named explicitly.
type DiskState struct {
	TotalBytes        int64
	FreeBytes         int64
	OurStorage        int64
	MaxPct            int
	TargetHeadroomPct int
	MinReserveGB      int
	HardMinGB         int
}

// DefaultDiskState returns a DiskState pre-populated with recommended
// defaults (caller still fills in Total/Free/OurStorage).
func DefaultDiskState() DiskState {
	return DiskState{
		MaxPct:            70,
		TargetHeadroomPct: 50,
		MinReserveGB:      5,
		HardMinGB:         1,
	}
}

// ComputeBudget returns the number of additional bytes the cache may
// hold given the current disk state.
func (d DiskState) ComputeBudget() int64 {
	external := d.TotalBytes - d.FreeBytes - d.OurStorage
	if external < 0 {
		external = 0
	}
	maxAllowed := d.TotalBytes * int64(d.MaxPct) / 100
	headroom := maxAllowed - external
	if headroom < 0 {
		headroom = 0
	}
	budget := headroom * int64(d.TargetHeadroomPct) / 100

	minReserveBytes := int64(d.MinReserveGB) * 1024 * 1024 * 1024
	if d.FreeBytes-budget < minReserveBytes {
		budget = d.FreeBytes - minReserveBytes
	}
	if budget < 0 {
		budget = 0
	}
	return budget
}

// CanStore reports whether the node should accept new writes at all.
func (d DiskState) CanStore() bool {
	hardMinBytes := int64(d.HardMinGB) * 1024 * 1024 * 1024
	return d.FreeBytes >= hardMinBytes && d.ComputeBudget() > 0
}

// Pressure returns a 0.0..1.0 gauge of how close to the ceiling we are.
func (d DiskState) Pressure() float64 {
	if d.TotalBytes <= 0 || d.MaxPct <= 0 {
		return 0
	}
	external := d.TotalBytes - d.FreeBytes - d.OurStorage
	if external < 0 {
		external = 0
	}
	maxAllowed := d.TotalBytes * int64(d.MaxPct) / 100
	if maxAllowed <= 0 {
		return 0
	}
	p := float64(external) / float64(maxAllowed)
	if p < 0 {
		p = 0
	}
	if p > 1 {
		p = 1
	}
	return p
}

// DiskStatter produces a raw disk snapshot for the path backing the
// cache.
type DiskStatter interface {
	DiskStats(path string) (totalBytes, freeBytes int64, err error)
}

// ManagerConfig collects the tunables that don't depend on the
// transient disk snapshot.
type ManagerConfig struct {
	MaxPct            int
	TargetHeadroomPct int
	MinReserveGB      int
	HardMinGB         int
	PollInterval      time.Duration
}

// DefaultManagerConfig returns the recommended Manager tunables:
// MaxPct=70, TargetHeadroomPct=50, MinReserveGB=5, HardMinGB=1,
// PollInterval=30s. Suitable for general-purpose nodes; override for
// dedicated storage hosts or tight-disk edges.
func DefaultManagerConfig() ManagerConfig {
	return ManagerConfig{
		MaxPct:            70,
		TargetHeadroomPct: 50,
		MinReserveGB:      5,
		HardMinGB:         1,
		PollInterval:      30 * time.Second,
	}
}

func (c ManagerConfig) normalized() ManagerConfig {
	if c.MaxPct <= 0 {
		c.MaxPct = 70
	}
	if c.TargetHeadroomPct <= 0 {
		c.TargetHeadroomPct = 50
	}
	if c.MinReserveGB <= 0 {
		c.MinReserveGB = 5
	}
	if c.HardMinGB <= 0 {
		c.HardMinGB = 1
	}
	if c.PollInterval <= 0 {
		c.PollInterval = 30 * time.Second
	}
	return c
}

// LowBudgetCallback is invoked when Refresh observes a budget low
// enough that the cache should start evicting.
type LowBudgetCallback func(budget int64)

// Manager polls disk state on an interval and serves the current quota
// answer. Safe for concurrent use.
type Manager struct {
	cfg     ManagerConfig
	path    string
	statter DiskStatter

	ourStorage int64
	budget     int64
	canStore   int32

	mu            sync.RWMutex
	lastState     DiskState
	lastStateTime time.Time

	cbMu sync.Mutex
	cb   LowBudgetCallback

	stopOnce sync.Once
	stopC    chan struct{}
	startC   sync.Once
}

// NewManager constructs a quota Manager. Pass statter=nil to use the
// platform default.
func NewManager(cfg ManagerConfig, cachePath string, statter DiskStatter) *Manager {
	cfg = cfg.normalized()
	if statter == nil {
		statter = defaultStatter{}
	}
	m := &Manager{
		cfg:     cfg,
		path:    cachePath,
		statter: statter,
		stopC:   make(chan struct{}),
	}
	_ = m.Refresh()
	return m
}

// SetOurStorage records the bytes currently cached.
func (m *Manager) SetOurStorage(bytes int64) {
	atomic.StoreInt64(&m.ourStorage, bytes)
	_ = m.Refresh()
}

// AddOurStorage atomically adjusts the cached-bytes counter.
func (m *Manager) AddOurStorage(delta int64) {
	atomic.AddInt64(&m.ourStorage, delta)
	_ = m.Refresh()
}

// OurStorage returns the caller-provided cached-bytes total.
func (m *Manager) OurStorage() int64 { return atomic.LoadInt64(&m.ourStorage) }

// Refresh polls disk stats and recomputes the budget.
func (m *Manager) Refresh() error {
	total, free, err := m.statter.DiskStats(m.path)
	if err != nil {
		return err
	}
	ours := atomic.LoadInt64(&m.ourStorage)
	d := DiskState{
		TotalBytes:        total,
		FreeBytes:         free,
		OurStorage:        ours,
		MaxPct:            m.cfg.MaxPct,
		TargetHeadroomPct: m.cfg.TargetHeadroomPct,
		MinReserveGB:      m.cfg.MinReserveGB,
		HardMinGB:         m.cfg.HardMinGB,
	}
	budget := d.ComputeBudget()
	canStore := d.CanStore()
	atomic.StoreInt64(&m.budget, budget)
	if canStore {
		atomic.StoreInt32(&m.canStore, 1)
	} else {
		atomic.StoreInt32(&m.canStore, 0)
	}
	m.mu.Lock()
	m.lastState = d
	m.lastStateTime = time.Now()
	m.mu.Unlock()

	m.cbMu.Lock()
	cb := m.cb
	m.cbMu.Unlock()
	if cb != nil && (budget == 0 || ours > budget) {
		cb(budget)
	}
	return nil
}

// CurrentBudget returns the most recently computed budget.
func (m *Manager) CurrentBudget() int64 { return atomic.LoadInt64(&m.budget) }

// CanStore reports whether new writes are permitted at all.
func (m *Manager) CanStore() bool { return atomic.LoadInt32(&m.canStore) == 1 }

// HasRoomFor reports whether nBytes fits within the current budget.
func (m *Manager) HasRoomFor(nBytes int64) bool {
	if !m.CanStore() {
		return false
	}
	cur := atomic.LoadInt64(&m.ourStorage)
	return cur+nBytes <= atomic.LoadInt64(&m.budget)
}

// Pressure reports the latest pressure value.
func (m *Manager) Pressure() float64 {
	m.mu.RLock()
	d := m.lastState
	m.mu.RUnlock()
	return d.Pressure()
}

// LastState returns a snapshot of the most recent disk reading.
func (m *Manager) LastState() DiskState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastState
}

// OnLowBudget registers a callback invoked on too-low budget.
func (m *Manager) OnLowBudget(cb LowBudgetCallback) {
	m.cbMu.Lock()
	m.cb = cb
	m.cbMu.Unlock()
}

// Start launches the background polling goroutine. Idempotent.
func (m *Manager) Start(ctx context.Context) {
	m.startC.Do(func() {
		go m.loop(ctx)
	})
}

// Stop halts the poll loop.
func (m *Manager) Stop() {
	m.stopOnce.Do(func() { close(m.stopC) })
}

func (m *Manager) loop(ctx context.Context) {
	t := time.NewTicker(m.cfg.PollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopC:
			return
		case <-t.C:
			_ = m.Refresh()
		}
	}
}

// Config exposes a read-only view of the manager's configuration.
func (m *Manager) Config() ManagerConfig { return m.cfg }
