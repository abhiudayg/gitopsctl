package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"aeswibon.com/github/gitopsctl/internal/common"
)

const (
	// DefaultAppConfigFile is the default path to store registered applications
	DefaultAppConfigFile = "configs/applications.json"
)

// Application represents a single GitOps application managed by the controller.
// It encapsulates all the necessary metadata and operational details required
// to monitor and synchronize the application's state between Git and Kubernetes.
type Application struct {
	Name                string             `json:"name"`
	RepoURL             string             `json:"repo_url"`
	Branch              string             `json:"branch"`
	Path                string             `json:"path"`
	ClusterName         string             `json:"cluster_name"`
	Interval            string             `json:"interval"`
	PollingInterval     time.Duration      `json:"-"`
	LastSyncedGitHash   string             `json:"last_synced_git_hash,omitempty"`
	Status              string             `json:"status,omitempty"`
	Message             string             `json:"message,omitempty"`
	ConsecutiveFailures int                `json:"consecutive_failures,omitempty"`
	SyncPolicy          string             `json:"sync_policy,omitempty"`
	Prune               bool               `json:"prune,omitempty"`
	ApprovedGitHash     string             `json:"approved_git_hash,omitempty"`
	LatestGitHash       string             `json:"latest_git_hash,omitempty"`
	WebhookURL          string             `json:"webhook_url,omitempty"`
	WebhookSecret       string             `json:"webhook_secret,omitempty"`
	AppliedResources    []ResourceMetadata `json:"applied_resources,omitempty"`
}

// ResourceMetadata matches the one in internal/core/k8s to avoid circular imports.
type ResourceMetadata struct {
	Group     string `json:"group"`
	Version   string `json:"version"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// Applications represents a collection of Application objects.
// It uses a mutex to ensure thread-safe access to the underlying map of applications.
type Applications struct {
	Apps map[string]*Application
	mu   sync.RWMutex
}

// NewApplications creates and initializes a new Applications collection.
// It returns an empty collection with a properly initialized map.
func NewApplications() *Applications {
	return &Applications{
		Apps: make(map[string]*Application),
	}
}

// Lock acquires a write lock on the Applications collection.
// This ensures exclusive access to the collection for write operations.
func (a *Applications) Lock() {
	a.mu.Lock()
}

// RLock acquires a read lock on the Applications collection.
// This allows multiple readers to access the collection concurrently,
// while preventing write operations until the read lock is released.
func (a *Applications) RLock() {
	a.mu.RLock()
}

// RUnlock releases the read lock held on the Applications collection.
// It should always be called after RLock, typically using a defer statement.
func (a *Applications) RUnlock() {
	a.mu.RUnlock()
}

// Unlock releases the write lock held on the Applications collection.
// It should always be called after Lock, typically using a defer statement.
func (a *Applications) Unlock() {
	a.mu.Unlock()
}

// Add adds a new application to the collection.
// The caller is responsible for acquiring the necessary write lock before calling this method.
func (a *Applications) Add(app *Application) {
	a.Apps[app.Name] = app
}

// Get retrieves an application by its name.
// The caller is responsible for acquiring the necessary read or write lock before calling this method.
func (a *Applications) Get(name string) (*Application, bool) {
	app, ok := a.Apps[name]
	return app, ok
}

// List returns a slice containing all applications in the collection.
// The caller is responsible for acquiring the necessary read or write lock before calling this method.
func (a *Applications) List() []*Application {
	list := make([]*Application, 0, len(a.Apps))
	for _, app := range a.Apps {
		list = append(list, app)
	}
	return list
}

// Len returns the number of applications in the collection.
func (a *Applications) Len() int {
	return len(a.Apps)
}

// Delete removes an application from the collection by its name.
// The caller is responsible for acquiring the necessary write lock before calling this method.
func (a *Applications) Delete(name string) {
	delete(a.Apps, name)
}

// LoadApplications loads applications from the specified JSON file.
// It initializes the Applications collection and populates it with data from the file.
// If the file does not exist, it returns an empty collection.
func LoadApplications(filePath string) (*Applications, error) {
	apps := NewApplications()
	apps.mu.Lock() // Acquire lock for initial load
	defer apps.mu.Unlock()

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return apps, nil // Return empty if file doesn't exist
		}
		return nil, fmt.Errorf("failed to read applications file %s: %w", filePath, err)
	}

	var loadedApps []*Application
	if err := json.Unmarshal(data, &loadedApps); err != nil {
		return nil, fmt.Errorf("failed to unmarshal applications data: %w", err)
	}

	for _, app := range loadedApps {
		// Parse interval string to time.Duration
		duration, err := time.ParseDuration(app.Interval)
		if err != nil {
			return nil, fmt.Errorf("invalid polling interval for app %s: %w", app.Name, err)
		}
		app.PollingInterval = duration
		apps.Apps[app.Name] = app // Directly add to map while lock is held
	}

	return apps, nil
}

// SaveApplications saves the current state of applications to the specified JSON file.
// The caller is responsible for acquiring the necessary lock before calling this method.
func SaveApplications(apps *Applications, filePath string) error {
	// Ensure the directory exists
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Convert map to slice for stable JSON output
	list := make([]*Application, 0, len(apps.Apps))
	for _, app := range apps.Apps {
		list = append(list, app)
	}

	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal applications data: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write applications file %s: %w", filePath, err)
	}
	return nil
}

// ToTableHeaders implements cliutils.Renderable for table output headers.
// It returns the headers for the table representation of the Application.
func (a *Application) ToTableHeaders(details bool) []string {
	if details {
		return []string{"NAME", "REPO URL", "BRANCH", "PATH", "CLUSTER", "INTERVAL", "STATUS", "SYNC POLICY", "LAST SYNCED HASH", "FAILURES", "MESSAGE"}
	}
	return []string{"NAME", "REPO URL", "BRANCH", "PATH", "CLUSTER", "INTERVAL"}
}

// ToTableRow implements cliutils.Renderable for table output rows.
// It returns a slice of strings representing the application data formatted for table display.
func (a *Application) ToTableRow(details bool) []string {
	hash := a.LastSyncedGitHash
	if len(hash) > 7 {
		hash = hash[:7]
	}
	if details {
		return []string{
			a.Name,
			common.TruncateString(a.RepoURL, 30),
			common.DefaultIfEmpty(a.Branch, "main"),
			common.TruncateString(a.Path, 20),
			a.ClusterName,
			a.Interval,
			a.Status,
			common.DefaultIfEmpty(a.SyncPolicy, "auto"),
			hash,
			fmt.Sprintf("%d", a.ConsecutiveFailures),
			common.TruncateString(a.Message, 40),
		}
	}
	return []string{
		a.Name,
		common.TruncateString(a.RepoURL, 30),
		common.DefaultIfEmpty(a.Branch, "main"),
		common.TruncateString(a.Path, 20),
		a.ClusterName,
		a.Interval,
	}
}

// ToJSONMap implements cliutils.Renderable for JSON output.
// It returns a map representation of the Application suitable for JSON serialization.
func (a *Application) ToJSONMap() map[string]any {
	return map[string]any{
		"name":                 a.Name,
		"repo_url":             a.RepoURL,
		"branch":               common.DefaultIfEmpty(a.Branch, "main"),
		"path":                 a.Path,
		"cluster":              a.ClusterName,
		"interval":             a.Interval,
		"status":               a.Status,
		"last_synced_hash":     a.LastSyncedGitHash,
		"consecutive_failures": a.ConsecutiveFailures,
		"message":              a.Message,
	}
}

// ToYAMLString implements cliutils.Renderable for YAML output.
// It returns a YAML-formatted string representation of the Application.
func (a *Application) ToYAMLString() string {
	// Build YAML string manually for simplicity
	return fmt.Sprintf(`name: %s
  repo_url: %s
  branch: %s
  path: %s
  cluster: %s
  interval: %s
  status: %s
  last_synced_hash: %s
  consecutive_failures: %d
  message: %s`,
		a.Name,
		a.RepoURL,
		common.DefaultIfEmpty(a.Branch, "main"),
		a.Path,
		a.ClusterName,
		a.Interval,
		a.Status,
		a.LastSyncedGitHash,
		a.ConsecutiveFailures,
		a.Message,
	)
}
