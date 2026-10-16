package tui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// AppResponse matches the JSON returned by GET /api/v1/applications
type AppResponse struct {
	Name                string `json:"name"`
	RepoURL             string `json:"repo_url"`
	Branch              string `json:"branch"`
	Path                string `json:"path"`
	ClusterName         string `json:"cluster_name"`
	Interval            string `json:"interval"`
	LastSyncedGitHash   string `json:"last_synced_git_hash"`
	LatestGitHash       string `json:"latest_git_hash"`
	Status              string `json:"status"`
	Message             string `json:"message"`
	ConsecutiveFailures int    `json:"consecutive_failures"`
	SyncPolicy          string `json:"sync_policy"`
	ApprovedGitHash     string `json:"approved_git_hash"`
}

// ClusterResponse matches the JSON returned by GET /api/v1/clusters
type ClusterResponse struct {
	Name           string    `json:"name"`
	KubeconfigPath string    `json:"kubeconfig_path"`
	RegisteredAt   time.Time `json:"registered_at"`
	Status         string    `json:"status"`
	Message        string    `json:"message"`
	LastCheckedAt  time.Time `json:"last_checked_at"`
}

type apiClient struct {
	baseURL   string
	client    *http.Client
	sseClient *http.Client
}

type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(req)
}

func newAPIClient(baseURL, token string) *apiClient {
	var transport http.RoundTripper
	if token != "" {
		transport = bearerTransport{token: token, base: http.DefaultTransport}
	}
	return &apiClient{
		baseURL:   baseURL,
		client:    &http.Client{Timeout: 10 * time.Second, Transport: transport},
		sseClient: &http.Client{Transport: transport},
	}
}

func (c *apiClient) getApplications() ([]AppResponse, error) {
	resp, err := c.client.Get(fmt.Sprintf("%s/api/v1/applications", c.baseURL))
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	var apps []AppResponse
	if err := json.NewDecoder(resp.Body).Decode(&apps); err != nil {
		return nil, err
	}
	return apps, nil
}

func (c *apiClient) getClusters() ([]ClusterResponse, error) {
	resp, err := c.client.Get(fmt.Sprintf("%s/api/v1/clusters", c.baseURL))
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	var clusters []ClusterResponse
	if err := json.NewDecoder(resp.Body).Decode(&clusters); err != nil {
		return nil, err
	}
	return clusters, nil
}

func (c *apiClient) syncApp(name string) error {
	resp, err := c.client.Post(
		fmt.Sprintf("%s/api/v1/applications/%s/sync", c.baseURL, name),
		"application/json", nil,
	)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	return nil
}

func (c *apiClient) unregisterApp(name string) error {
	req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/applications/%s", c.baseURL, name), nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	return nil
}

func (c *apiClient) checkCluster(name string) error {
	resp, err := c.client.Post(
		fmt.Sprintf("%s/api/v1/clusters/%s/check", c.baseURL, name),
		"application/json", nil,
	)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	return nil
}

func (c *apiClient) approveApp(name, commitHash string) error {
	payload, _ := json.Marshal(map[string]string{"commitHash": commitHash})
	resp, err := c.client.Post(
		fmt.Sprintf("%s/api/v1/applications/%s/approve", c.baseURL, name),
		"application/json",
		strings.NewReader(string(payload)),
	)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	return nil
}

func (c *apiClient) unregisterCluster(name string) error {
	req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/v1/clusters/%s", c.baseURL, name), nil)
	if err != nil {
		return err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	return nil
}

// Event represents an integration event from the server.
type Event struct {
	ID     string         `json:"id"`
	Type   string         `json:"type"`
	Source string         `json:"source"`
	Time   time.Time      `json:"time"`
	Data   map[string]any `json:"data"`
}

func (c *apiClient) getHistory() ([]Event, error) {
	resp, err := c.client.Get(fmt.Sprintf("%s/api/v1/events/history", c.baseURL))
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	var history []Event
	if err := json.NewDecoder(resp.Body).Decode(&history); err != nil {
		return nil, err
	}
	return history, nil
}
