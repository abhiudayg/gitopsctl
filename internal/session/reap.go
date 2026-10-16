package session

import (
	"context"
	"time"

	"go.uber.org/zap"
)

func (m *Manager) Run(ctx context.Context, interval time.Duration) {
	m.Reap(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.Reap(ctx)
		}
	}
}

// Reap tears down expired, stuck and half-created sessions, then removes labelled namespaces that no session record owns.
func (m *Manager) Reap(ctx context.Context) {
	m.mu.Lock()
	entries := make([]*entry, 0, len(m.entries))
	for _, e := range m.entries {
		entries = append(entries, e)
	}
	m.mu.Unlock()

	for _, e := range entries {
		m.reapEntry(ctx, e)
	}
	m.reapOrphans(ctx)
}

func (m *Manager) reapEntry(ctx context.Context, e *entry) {
	if !e.op.TryLock() {
		return
	}
	defer e.op.Unlock()
	s := m.snap(e)
	if s.State == StateActive && m.now().Before(s.ExpiresAt) {
		return
	}
	m.logger.Info("Tearing down session", zap.String("session", s.ID), zap.String("state", string(s.State)))
	if _, err := m.teardownLocked(ctx, e); err != nil {
		m.logger.Warn("Session teardown failed; will retry", zap.String("session", s.ID), zap.Error(err))
	}
}

func (m *Manager) reapOrphans(ctx context.Context) {
	clusters := map[string]bool{}
	for _, agent := range m.cfg.Agents {
		clusters[agent.Cluster] = true
	}
	for name := range clusters {
		cluster, err := m.clusterFor(name)
		if err != nil {
			m.logger.Warn("Orphan scan skipped", zap.String("cluster", name), zap.Error(err))
			continue
		}
		namespaces, err := cluster.Namespaces(ctx)
		if err != nil {
			m.logger.Warn("Orphan scan failed", zap.String("cluster", name), zap.Error(err))
			continue
		}
		for _, ns := range namespaces {
			if m.known(ns.Session) {
				continue
			}
			m.logger.Warn("Deleting orphaned session namespace", zap.String("namespace", ns.Name), zap.String("session", ns.Session))
			if err := cluster.DeleteNamespace(ctx, ns.Name, ns.Session); err != nil {
				m.logger.Warn("Orphan deletion failed", zap.String("namespace", ns.Name), zap.Error(err))
			}
		}
	}
}

func (m *Manager) known(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.entries[id]
	return ok
}
