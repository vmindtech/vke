package request

import (
	"encoding/json"
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

// UnmarshalJSON accepts VKE camelCase keys and common alternates (e.g. apiAccess, snake_case)
// so fields are not left as zero values when upstream uses a different naming convention.
func (r *CreateClusterRequest) UnmarshalJSON(data []byte) error {
	type raw struct {
		ClusterName       string   `json:"clusterName"`
		ProjectID         string   `json:"projectId"`
		K8s               string   `json:"kubernetesVersion"`
		K8sSnake          string   `json:"kubernetes_version"`
		NodeKeyPair       string   `json:"nodeKeyPairName"`
		NodeKeySnake      string   `json:"node_key_pair_name"`
		ClusterAPIAccess  string   `json:"clusterApiAccess"`
		APIAccess         string   `json:"apiAccess"`
		ClusterAPISnake   string   `json:"cluster_api_access"`
		SubnetIDs         []string `json:"subnetIds"`
		MinSize           int      `json:"workerNodeGroupMinSize"`
		MinSizeSnake      int      `json:"worker_node_group_min_size"`
		MaxSize           int      `json:"workerNodeGroupMaxSize"`
		MaxSizeSnake      int      `json:"worker_node_group_max_size"`
		WorkerFlavor      string   `json:"workerInstanceFlavorUUID"`
		WorkerFlavorSnake string   `json:"worker_instance_flavor_uuid"`
		MasterFlavor      string   `json:"masterInstanceFlavorUUID"`
		MasterFlavorSnake string   `json:"master_instance_flavor_uuid"`
		DiskGB            int      `json:"workerDiskSizeGB"`
		DiskGBSnake       int      `json:"worker_disk_size_gb"`
		AllowedCIDRs      []string `json:"allowedCIDRs"`
	}
	var w raw
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	r.ClusterName = w.ClusterName
	r.ProjectID = w.ProjectID
	r.KubernetesVersion = firstNonEmpty(w.K8s, w.K8sSnake)
	r.NodeKeyPairName = firstNonEmpty(w.NodeKeyPair, w.NodeKeySnake)
	r.ClusterAPIAccess = firstNonEmpty(w.ClusterAPIAccess, firstNonEmpty(w.APIAccess, w.ClusterAPISnake))
	r.SubnetIDs = w.SubnetIDs
	r.WorkerNodeGroupMinSize = pickInt(w.MinSize, w.MinSizeSnake)
	r.WorkerNodeGroupMaxSize = pickInt(w.MaxSize, w.MaxSizeSnake)
	r.WorkerInstanceFlavorUUID = firstNonEmpty(w.WorkerFlavor, w.WorkerFlavorSnake)
	r.MasterInstanceFlavorUUID = firstNonEmpty(w.MasterFlavor, w.MasterFlavorSnake)
	r.WorkerDiskSizeGB = pickInt(w.DiskGB, w.DiskGBSnake)
	r.AllowedCIDRS = w.AllowedCIDRs
	return nil
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func pickInt(primary, fallback int) int {
	if primary != 0 {
		return primary
	}
	return fallback
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
