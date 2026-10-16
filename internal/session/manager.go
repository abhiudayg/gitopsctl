package session

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"abhiudayg.com/github/gitopsctl/internal/common"
	"abhiudayg.com/github/gitopsctl/internal/core/k8s"
	"go.uber.org/zap"
)

type State string

const (
	StateCreating    State = "creating"
	StateActive      State = "active"
	StateTearingDown State = "tearing-down"
)

var (
	ErrNotFound  = errors.New("session not found")
	ErrLimit     = errors.New("session limit reached")
	ErrForbidden = errors.New("not permitted")
	ErrInvalid   = errors.New("invalid request")
	ErrNotActive = errors.New("session is not active")
)

var refPattern = regexp.MustCompile(`^[A-Za-z0-9._][A-Za-z0-9._/-]{0,199}$`)

type Session struct {
	ID         string                 `json:"id"`
	Agent      string                 `json:"agent"`
	Cluster    string                 `json:"cluster"`
	Namespace  string                 `json:"namespace"`
	State      State                  `json:"state"`
	CreatedAt  time.Time              `json:"created_at"`
	ExpiresAt  time.Time              `json:"expires_at"`
	Repo       string                 `json:"repo,omitempty"`
	Ref        string                 `json:"ref,omitempty"`
	Path       string                 `json:"path,omitempty"`
	Commit     string                 `json:"commit,omitempty"`
	DeployedAt time.Time              `json:"deployed_at,omitempty"`
	Resources  []k8s.ResourceMetadata `json:"resources,omitempty"`
}

type ApplyRequest struct {
	Cluster      string
	Namespace    string
	Session      string
	ManifestsDir string
	Previous     []k8s.ResourceMetadata
}

type Cluster interface {
	EnsureNamespace(ctx context.Context, spec k8s.SessionNamespaceSpec) error
	DeleteNamespace(ctx context.Context, name, session string) error
	Namespaces(ctx context.Context) ([]k8s.SessionNamespace, error)
	Apply(ctx context.Context, req ApplyRequest) ([]k8s.ResourceMetadata, error)
}

type ClusterFor func(name string) (Cluster, error)

type Fetcher func(ctx context.Context, repo, ref string) (dir, commit string, cleanup func(), err error)

type DeployRequest struct {
	Repo string
	Ref  string
	Path string
}

type entry struct {
	op sync.Mutex
	s  Session
}

type Manager struct {
	logger     *zap.Logger
	cfg        *Config
	clusterFor ClusterFor
	fetch      Fetcher
	file       string
	now        func() time.Time

	mu      sync.Mutex
	entries map[string]*entry
}

type Option func(*Manager)

func WithFetcher(f Fetcher) Option { return func(m *Manager) { m.fetch = f } }

func WithClock(now func() time.Time) Option { return func(m *Manager) { m.now = now } }

func NewManager(logger *zap.Logger, cfg *Config, clusterFor ClusterFor, file string, opts ...Option) (*Manager, error) {
	m := &Manager{
		logger:     logger,
		cfg:        cfg,
		clusterFor: clusterFor,
		fetch:      gitFetcher(logger),
		file:       file,
		now:        time.Now,
		entries:    map[string]*entry{},
	}
	for _, opt := range opts {
		opt(m)
	}
	if err := m.load(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manager) Create(ctx context.Context, agentID string, ttl time.Duration) (Session, error) {
	agent, ok := m.cfg.Agent(agentID)
	if !ok {
		return Session{}, fmt.Errorf("%w: unknown agent %q", ErrForbidden, agentID)
	}
	if ttl == 0 {
		ttl = time.Duration(agent.DefaultTTL)
	}
	if ttl < 0 || ttl > time.Duration(agent.MaxTTL) {
		return Session{}, fmt.Errorf("%w: ttl must be between 1s and %s", ErrInvalid, time.Duration(agent.MaxTTL))
	}
	cluster, err := m.clusterFor(agent.Cluster)
	if err != nil {
		return Session{}, fmt.Errorf("cluster %q: %w", agent.Cluster, err)
	}

	id, err := newID()
	if err != nil {
		return Session{}, err
	}
	now := m.now()
	e := &entry{s: Session{
		ID:        id,
		Agent:     agentID,
		Cluster:   agent.Cluster,
		Namespace: NamespaceName(agentID, id),
		State:     StateCreating,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
	}}
	e.op.Lock()
	defer e.op.Unlock()

	m.mu.Lock()
	if m.countLocked(agentID) >= agent.MaxSessions {
		m.mu.Unlock()
		return Session{}, fmt.Errorf("%w: agent %q already has %d sessions", ErrLimit, agentID, agent.MaxSessions)
	}
	m.entries[id] = e
	err = m.persistLocked()
	m.mu.Unlock()
	if err != nil {
		m.forget(id)
		return Session{}, err
	}

	spec := k8s.SessionNamespaceSpec{Name: e.s.Namespace, Session: id, Agent: agentID, Quota: agent.Quota}
	if err := cluster.EnsureNamespace(ctx, spec); err != nil {
		m.logger.Warn("Session namespace creation failed; leaving it for the janitor", zap.String("session", id), zap.Error(err))
		return Session{}, fmt.Errorf("create session namespace: %w", err)
	}
	return m.set(e, func(s *Session) { s.State = StateActive })
}

func (m *Manager) Get(id, agent string) (Session, error) {
	e, err := m.lookup(id, agent)
	if err != nil {
		return Session{}, err
	}
	return m.snap(e), nil
}

func (m *Manager) List(agent string) []Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	var sessions []Session
	for _, e := range m.entries {
		if agent == "" || e.s.Agent == agent {
			sessions = append(sessions, e.s)
		}
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].CreatedAt.Before(sessions[j].CreatedAt) })
	return sessions
}

func (m *Manager) Deploy(ctx context.Context, id, agentID string, req DeployRequest) (Session, error) {
	e, err := m.lookup(id, agentID)
	if err != nil {
		return Session{}, err
	}
	e.op.Lock()
	defer e.op.Unlock()
	s := m.snap(e)
	if s.State != StateActive || !m.now().Before(s.ExpiresAt) {
		return Session{}, ErrNotActive
	}
	agent, ok := m.cfg.Agent(s.Agent)
	if !ok {
		return Session{}, fmt.Errorf("%w: agent %q is no longer configured", ErrForbidden, s.Agent)
	}
	if !agent.allowsRepo(req.Repo) {
		return Session{}, fmt.Errorf("%w: repository is not allowed for this agent", ErrForbidden)
	}
	path, err := cleanPath(req.Path)
	if err != nil {
		return Session{}, err
	}
	if !refPattern.MatchString(req.Ref) || strings.Contains(req.Ref, "..") {
		return Session{}, fmt.Errorf("%w: ref must be a plain branch name", ErrInvalid)
	}
	cluster, err := m.clusterFor(s.Cluster)
	if err != nil {
		return Session{}, fmt.Errorf("cluster %q: %w", s.Cluster, err)
	}

	dir, commit, cleanup, err := m.fetch(ctx, req.Repo, req.Ref)
	if err != nil {
		return Session{}, fmt.Errorf("fetch repository: %w", err)
	}
	defer cleanup()
	manifestsDir, err := resolveInside(dir, path)
	if err != nil {
		return Session{}, err
	}

	applied, err := cluster.Apply(ctx, ApplyRequest{Cluster: s.Cluster, Namespace: s.Namespace, Session: id, ManifestsDir: manifestsDir, Previous: s.Resources})
	if err != nil {
		return Session{}, err
	}
	return m.set(e, func(s *Session) {
		s.Repo, s.Ref, s.Path, s.Commit = req.Repo, req.Ref, path, commit
		s.DeployedAt = m.now()
		s.Resources = applied
	})
}

func (m *Manager) Teardown(ctx context.Context, id, agentID string) (Session, error) {
	e, err := m.lookup(id, agentID)
	if err != nil {
		return Session{}, err
	}
	e.op.Lock()
	defer e.op.Unlock()
	return m.teardownLocked(ctx, e)
}

func (m *Manager) teardownLocked(ctx context.Context, e *entry) (Session, error) {
	s, err := m.set(e, func(s *Session) { s.State = StateTearingDown })
	if err != nil {
		return s, err
	}
	cluster, err := m.clusterFor(s.Cluster)
	if err != nil {
		return s, fmt.Errorf("cluster %q: %w", s.Cluster, err)
	}
	if err := cluster.DeleteNamespace(ctx, s.Namespace, s.ID); err != nil {
		return s, fmt.Errorf("teardown %s: %w", s.ID, err)
	}
	m.forget(s.ID)
	return s, nil
}

func (m *Manager) set(e *entry, change func(*Session)) (Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	change(&e.s)
	return e.s, m.persistLocked()
}

func (m *Manager) snap(e *entry) Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return e.s
}

func (m *Manager) lookup(id, agent string) (*entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok {
		return nil, ErrNotFound
	}
	if agent != "" && e.s.Agent != agent {
		return nil, ErrNotFound
	}
	return e, nil
}

func (m *Manager) countLocked(agent string) int {
	n := 0
	for _, e := range m.entries {
		if e.s.Agent == agent {
			n++
		}
	}
	return n
}

func (m *Manager) forget(id string) {
	m.mu.Lock()
	delete(m.entries, id)
	err := m.persistLocked()
	m.mu.Unlock()
	if err != nil {
		m.logger.Error("Failed to persist sessions", zap.Error(err))
	}
}

func (m *Manager) load() error {
	data, err := os.ReadFile(m.file)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read sessions file: %w", err)
	}
	var sessions []Session
	if err := jsonUnmarshal(data, &sessions); err != nil {
		return fmt.Errorf("parse sessions file %s: %w", m.file, err)
	}
	for _, s := range sessions {
		m.entries[s.ID] = &entry{s: s}
	}
	return nil
}

func (m *Manager) persistLocked() error {
	sessions := make([]Session, 0, len(m.entries))
	for _, e := range m.entries {
		sessions = append(sessions, e.s)
	}
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].ID < sessions[j].ID })
	data, err := jsonMarshal(sessions)
	if err != nil {
		return err
	}
	return common.WriteFileAtomic(m.file, data, 0o600)
}

func newID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func cleanPath(p string) (string, error) {
	if p == "" {
		return ".", nil
	}
	cleaned := filepath.Clean(p)
	if filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: path must stay inside the repository", ErrInvalid)
	}
	return cleaned, nil
}

func resolveInside(root, rel string) (string, error) {
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve repository: %w", err)
	}
	target, err := filepath.EvalSymlinks(filepath.Join(resolvedRoot, rel))
	if err != nil {
		return "", fmt.Errorf("%w: manifest path: %v", ErrInvalid, err)
	}
	if target != resolvedRoot && !strings.HasPrefix(target, resolvedRoot+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: path must stay inside the repository", ErrInvalid)
	}
	if err := rejectSymlinks(target); err != nil {
		return "", err
	}
	return target, nil
}

func rejectSymlinks(dir string) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: symlinks are not allowed in manifests", ErrInvalid)
		}
		return nil
	})
}
