package cluster

import (
	"time"

	clustercore "abhiudayg.com/github/gitopsctl/internal/core/cluster"
)

// RegisterRequest defines the payload for registering a new cluster.
// This structure is used in the API requests to register a new Kubernetes cluster with the GitOps controller.
type RegisterRequest struct {
	// Name is the unique identifier for the cluster.
	Name string `json:"name" validate:"required"`
	// KubeconfigPath is the file path to the kubeconfig file for accessing the Kubernetes cluster.
	KubeconfigPath string `json:"kubeconfig_path" validate:"required,kubeconfigfile"`
}

// Response defines the structure for returning cluster details via the API.
// This structure is used in the API responses to provide information about registered clusters.
type Response struct {
	// Name is the unique identifier for the cluster.
	Name string `json:"name"`
	// KubeconfigPath is the file path to the kubeconfig file for accessing the Kubernetes cluster.
	KubeconfigPath string `json:"kubeconfig_path"`
	// RegisteredAt is the timestamp when the cluster was registered with the GitOps controller.
	RegisteredAt time.Time `json:"registered_at"`
	// Status indicates the current status of the cluster (e.g., "active", "inactive", "error").
	Status string `json:"status"`
	// Message provides additional information about the cluster's status, such as error messages or warnings.
	Message string `json:"message"`
	// LastCheckedAt is the timestamp of the last health check performed on the cluster.
	LastCheckedAt time.Time `json:"last_checked_at"`
}

// HealthCheckTriggerResponse represents the response for health check trigger requests.
type HealthCheckTriggerResponse struct {
	Message string `json:"message"`
	Status  string `json:"status"`
}

// ErrorResponse represents an error response.
type ErrorResponse struct {
	Message string `json:"message"`
}

// ConvertToResponse converts a Cluster to a Response.
func ConvertToResponse(cl *clustercore.Cluster) Response {
	return Response{
		Name:           cl.Name,
		KubeconfigPath: cl.KubeconfigPath,
		RegisteredAt:   cl.RegisteredAt,
		Status:         cl.Status,
		Message:        cl.Message,
		LastCheckedAt:  cl.LastCheckedAt,
	}
}
