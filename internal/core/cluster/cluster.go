package cluster

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"abhiudayg.com/github/gitopsctl/internal/common"
)

const (
	// DefaultClusterHealthCheckInterval is the default interval for checking cluster health.
	DefaultClusterHealthCheckInterval = 5 * time.Minute
	// DefaultClusterConfigFile is the default path to store registered clusters
	DefaultClusterConfigFile = "configs/clusters.json"
)

// Cluster represents a registered Kubernetes cluster.
// It contains the cluster name, path to the kubeconfig file, registration time,
// and optional status and message fields for error handling or status reporting.
type Cluster struct {
	Name              string    `json:"name"`
	KubeconfigPath    string    `json:"kubeconfig_path"`
	RegisteredAt      time.Time `json:"registered_at"`
	Status            string    `json:"status,omitempty"`
	Message           string    `json:"message,omitempty"`
	LastCheckedAt     time.Time `json:"last_checked_at,omitempty"`
	AllowedNamespaces []string  `json:"allowed_namespaces,omitempty"`
}

// Clusters represents a thread-safe collection of Cluster objects.
// It provides methods to add, retrieve, list, and delete clusters.
// The collection is protected by a read-write mutex to allow concurrent access.
type Clusters struct {
	Cs map[string]*Cluster
	mu sync.RWMutex
}

// NewClusters creates a new empty Clusters collection.
// It initializes the map to hold Cluster objects and returns a pointer to the Clusters instance.
func NewClusters() *Clusters {
	return &Clusters{
		Cs: make(map[string]*Cluster),
	}
}

// Lock method acquires a write lock on the Clusters collection.
// This prevents other goroutines from reading or writing to the collection
// while the lock is held, ensuring thread safety during modifications.
func (c *Clusters) Lock() {
	c.mu.Lock()
}

// RLock method acquires a read lock on the Clusters collection.
// This allows multiple readers to access the collection concurrently,
// but prevents any writes while the read lock is held.
func (c *Clusters) RLock() {
	c.mu.RLock()
}

// Unlock method releases the write lock on the Clusters collection.
// It should be called after any modifications to the collection.
func (c *Clusters) Unlock() {
	c.mu.Unlock()
}

// RUnlock method releases the read lock on the Clusters collection.
// It should be called after read operations on the collection.
func (c *Clusters) RUnlock() {
	c.mu.RUnlock()
}

// Add adds a cluster to the collection.
// If a cluster with the same name already exists, it will be overwritten.
// This method is not thread-safe and should be called with the write lock held.
func (c *Clusters) Add(cluster *Cluster) {
	c.Cs[cluster.Name] = cluster
}

// Get retrieves a cluster by name.
// If the cluster exists, it returns the cluster and true.
// If the cluster does not exist, it returns nil and false.
func (c *Clusters) Get(name string) (*Cluster, bool) {
	cluster, ok := c.Cs[name]
	return cluster, ok
}

// List returns a slice of all clusters.
// It returns a slice of pointers to Cluster objects.
func (c *Clusters) List() []*Cluster {
	list := make([]*Cluster, 0, len(c.Cs))
	for _, cluster := range c.Cs {
		list = append(list, cluster)
	}
	return list
}

// Delete removes a cluster by name.
// If the cluster does not exist, it does nothing.
func (c *Clusters) Delete(name string) {
	delete(c.Cs, name)
}

// Len returns the number of clusters in the collection.
func (c *Clusters) Len() int {
	return len(c.Cs)
}

// LoadClusters loads clusters from the specified file path.
// It reads the JSON data from the file, unmarshals it into Cluster objects,
// and populates the Clusters collection. If the file does not exist, it returns an empty collection.
// This function acquires its own lock as it's typically called at startup.
func LoadClusters(filePath string) (*Clusters, error) {
	clusters := NewClusters()
	clusters.mu.Lock()
	defer clusters.mu.Unlock()

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return clusters, nil
		}
		return nil, fmt.Errorf("failed to read clusters file %s: %w", filePath, err)
	}

	var loadedClusters []*Cluster
	if err := json.Unmarshal(data, &loadedClusters); err != nil {
		return nil, fmt.Errorf("failed to unmarshal clusters data: %w", err)
	}

	for _, cluster := range loadedClusters {
		clusters.Cs[cluster.Name] = cluster
	}

	return clusters, nil
}

// SaveClusters saves the current state of clusters to the specified file path.
// It serializes the Clusters collection to JSON and writes it to the file.
// If the directory does not exist, it creates it.
// This function does not acquire its own lock, so it should be called with the appropriate lock held.
func SaveClusters(clusters *Clusters, filePath string) error {
	// IMPORTANT: No locking here. The caller is responsible for acquiring the appropriate lock.

	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	list := make([]*Cluster, 0, len(clusters.Cs))
	for _, cluster := range clusters.Cs {
		list = append(list, cluster)
	}

	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal clusters data: %w", err)
	}

	if err := common.WriteFileAtomic(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write clusters file %s: %w", filePath, err)
	}
	return nil
}

// VerifyCluster checks if a cluster with the given name exists in the collection.
// It loads the clusters from the default configuration file and checks if the specified cluster exists.
func VerifyCluster(clusterName string) (*Cluster, bool, error) {
	clusters, err := LoadClusters(DefaultClusterConfigFile)
	if err != nil {
		return nil, false, fmt.Errorf("failed to load cluster configurations: %w", err)
	}

	clusters.RLock()
	defer clusters.RUnlock()

	cluster, exists := clusters.Get(clusterName)
	if !exists {
		return nil, false, fmt.Errorf("cluster '%s' not found\nUse 'gitopsctl list-clusters' to see registered clusters or 'gitopsctl register-cluster' to add one", clusterName)
	}
	return cluster, exists, nil
}

// ToTableHeaders implements cliutils.Renderable for table output headers.
// It returns the headers for the table based on whether detailed output is requested.
func (c *Cluster) ToTableHeaders(details bool) []string {
	if details {
		return []string{"NAME", "STATUS", "KUBECONFIG", "MESSAGE", "REGISTERED", "LAST CHECKED"}
	}
	return []string{"NAME", "STATUS", "KUBECONFIG", "REGISTERED"}
}

// ToTableRow implements cliutils.Renderable for table output rows.
// It formats the cluster information into a slice of strings for table display.
func (c *Cluster) ToTableRow(details bool) []string {
	lastChecked := "N/A"
	if !c.LastCheckedAt.IsZero() {
		lastChecked = c.LastCheckedAt.Format("2006-01-02 15:04:05 MST") // Consistent time format
	}
	status := formatClusterStatus(c.Status)

	if details {
		return []string{
			c.Name,
			status,
			common.TruncateString(c.KubeconfigPath, 30),
			common.TruncateString(c.Message, 40),
			c.RegisteredAt.Format("2006-01-02 15:04:05 MST"), // Consistent time format
			lastChecked,
		}
	}
	return []string{
		c.Name,
		status,
		common.TruncateString(c.KubeconfigPath, 40),
		c.RegisteredAt.Format("2006-01-02 15:04:05 MST"), // Consistent time format
	}
}

// ToJSONMap implements cliutils.Renderable for JSON output.
// It formats the cluster information into a map suitable for JSON serialization.
func (c *Cluster) ToJSONMap() map[string]any {
	lastCheckedAt := ""
	if !c.LastCheckedAt.IsZero() {
		lastCheckedAt = c.LastCheckedAt.Format(time.RFC3339)
	}
	return map[string]any{
		"name":            c.Name,
		"status":          c.Status,
		"kubeconfig_path": c.KubeconfigPath,
		"message":         c.Message,
		"registered_at":   c.RegisteredAt.Format(time.RFC3339),
		"last_checked_at": lastCheckedAt,
	}
}

// ToYAMLString implements cliutils.Renderable for YAML output.
// It formats the cluster information into a YAML string representation.
func (c *Cluster) ToYAMLString() string {
	lastCheckedAt := "N/A"
	if !c.LastCheckedAt.IsZero() {
		lastCheckedAt = c.LastCheckedAt.Format("2006-01-02 15:04:05 MST")
	}
	return fmt.Sprintf(`name: %s
  status: %s
  kubeconfig_path: %s
  message: %s
  registered_at: %s
  last_checked_at: %s`,
		c.Name,
		c.Status,
		c.KubeconfigPath,
		c.Message,
		c.RegisteredAt.Format("2006-01-02 15:04:05 MST"),
		lastCheckedAt,
	)
}

// formatClusterStatus provides a formatted status string with emojis.
// This function remains in cluster package as it's specific to cluster status logic.
func formatClusterStatus(status string) string {
	switch strings.ToLower(status) {
	case "active", "connected", "ready":
		return "✅ " + status
	case "inactive", "disconnected", "unreachable":
		return "❌ " + status
	case "pending", "connecting", "checkrequested":
		return "⏳ " + status
	case "error", "failed":
		return "❗ " + status
	default:
		return "❓ " + status
	}
}
