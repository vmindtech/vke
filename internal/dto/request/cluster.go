package request

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type CreateClusterRequest struct {
	ClusterName              string   `json:"clusterName" validate:"required,max=50"`
	ProjectID                string   `json:"projectId" validate:"required"`
	KubernetesVersion        string   `json:"kubernetesVersion" validate:"required,max=30"`
	NodeKeyPairName          string   `json:"nodeKeyPairName" validate:"required,max=140"`
	ClusterAPIAccess         string   `json:"clusterApiAccess" validate:"required,max=255"`
	SubnetIDs                []string `json:"subnetIds" validate:"required"`
	WorkerNodeGroupMinSize   int      `json:"workerNodeGroupMinSize" validate:"required,min=1"`
	WorkerNodeGroupMaxSize   int      `json:"workerNodeGroupMaxSize" validate:"required,min=1"`
	WorkerInstanceFlavorUUID string   `json:"workerInstanceFlavorUUID" validate:"required"`
	MasterInstanceFlavorUUID string   `json:"masterInstanceFlavorUUID" validate:"required"`
	WorkerDiskSizeGB         int      `json:"workerDiskSizeGB" validate:"required,min=20"`
	AllowedCIDRS             []string `json:"allowedCIDRs" validate:"required"`
}

// ParseCreateClusterRequestJSON decodes the VKE create-cluster JSON once (no Fiber BodyParser quirks),
// then fills any fields that are still empty from common alternate keys (snake_case, apiAccess, etc.).
func ParseCreateClusterRequestJSON(data []byte) (CreateClusterRequest, error) {
	var req CreateClusterRequest
	if len(bytes.TrimSpace(data)) == 0 {
		return req, fmt.Errorf("empty create cluster JSON")
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return req, err
	}
	enrichCreateClusterRequestFromJSONMap(data, &req)
	return req, nil
}

func enrichCreateClusterRequestFromJSONMap(data []byte, r *CreateClusterRequest) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return
	}
	str := func(dst *string, keys ...string) {
		if strings.TrimSpace(*dst) != "" {
			return
		}
		for _, k := range keys {
			raw, ok := m[k]
			if !ok {
				continue
			}
			var s string
			if json.Unmarshal(raw, &s) == nil && strings.TrimSpace(s) != "" {
				*dst = strings.TrimSpace(s)
				return
			}
		}
	}
	intIfZero := func(dst *int, keys ...string) {
		if *dst != 0 {
			return
		}
		for _, k := range keys {
			raw, ok := m[k]
			if !ok {
				continue
			}
			var n int
			if json.Unmarshal(raw, &n) == nil && n != 0 {
				*dst = n
				return
			}
			var f float64
			if json.Unmarshal(raw, &f) == nil && f != 0 {
				*dst = int(f)
				return
			}
		}
	}

	str(&r.ClusterName, "clusterName", "cluster_name")
	str(&r.ProjectID, "projectId", "project_id")
	str(&r.KubernetesVersion, "kubernetesVersion", "kubernetes_version")
	str(&r.NodeKeyPairName, "nodeKeyPairName", "node_key_pair_name")
	str(&r.ClusterAPIAccess, "clusterApiAccess", "cluster_api_access", "apiAccess")
	str(&r.WorkerInstanceFlavorUUID, "workerInstanceFlavorUUID", "worker_instance_flavor_uuid")
	str(&r.MasterInstanceFlavorUUID, "masterInstanceFlavorUUID", "master_instance_flavor_uuid")

	intIfZero(&r.WorkerNodeGroupMinSize, "workerNodeGroupMinSize", "worker_node_group_min_size")
	intIfZero(&r.WorkerNodeGroupMaxSize, "workerNodeGroupMaxSize", "worker_node_group_max_size")
	intIfZero(&r.WorkerDiskSizeGB, "workerDiskSizeGB", "worker_disk_size_gb")

	if len(r.SubnetIDs) == 0 {
		for _, k := range []string{"subnetIds", "subnet_ids"} {
			if raw, ok := m[k]; ok {
				var xs []string
				if json.Unmarshal(raw, &xs) == nil && len(xs) > 0 {
					r.SubnetIDs = xs
					break
				}
			}
		}
	}
	if len(r.AllowedCIDRS) == 0 {
		for _, k := range []string{"allowedCIDRs", "allowed_cidrs"} {
			if raw, ok := m[k]; ok {
				var xs []string
				if json.Unmarshal(raw, &xs) == nil && len(xs) > 0 {
					r.AllowedCIDRS = xs
					break
				}
			}
		}
	}
}

// NormalizeCreateClusterRequest sets DB-safe enum for cluster_api_access (MySQL enum public|private).
func NormalizeCreateClusterRequest(r *CreateClusterRequest) {
	if r == nil {
		return
	}
	r.ClusterAPIAccess = NormalizeClusterAPIAccessEnum(r.ClusterAPIAccess)
}

// NormalizeClusterAPIAccessEnum maps empty/unknown to public; private kept.
func NormalizeClusterAPIAccessEnum(s string) string {
	v := strings.ToLower(strings.TrimSpace(s))
	switch v {
	case "private":
		return "private"
	default:
		return "public"
	}
}

type CreateKubeconfigRequest struct {
	ClusterID  string `json:"clusterId" validate:"required"`
	KubeConfig string `json:"kubeconfig"`
}

type UpdateKubeconfigRequest struct {
	KubeConfig string `json:"kubeconfig" validate:"required"`
}

type UpdateClusterRequest struct {
	ClusterName                  string    `json:"cluster_name" validate:"required,max=50"`
	ClusterVersion               string    `json:"cluster_version" validate:"omitempty,max=30"`
	ClusterStatus                string    `json:"cluster_status" validate:"required,max=10"`
	ClusterAPIAccess             string    `json:"cluster_api_access" validate:"required,max=255"`
	ClusterCertificateExpireDate time.Time `json:"cluster_certificate_expire_date" validate:"required"`
}
