package resource

import (
	"time"
)

type AppResource struct {
	App     string    `json:"app"`
	Env     string    `json:"env"`
	Version string    `json:"version"`
	Time    time.Time `json:"time"`
}

type ClusterInfoResource struct {
	ClusterName string `json:"cluster_name"`
	ClusterID   string `json:"cluster_id"`
}

type CreateClusterResponse struct {
	ClusterID string `json:"clusterId"`
	ProjectID string `json:"projectId"`
}

type CreateComputeResponse struct {
	Server Server `json:"server"`
}

type Server struct {
	ID string `json:"id"`
}
