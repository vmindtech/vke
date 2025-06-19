package resource

import "time"

type ClusterInfoResource struct {
	ClusterName string `json:"cluster_name"`
	ClusterID   string `json:"cluster_id"`
}

type CreateClusterResponse struct {
	ClusterUUID   string `json:"cluster_uuid"`
	ClusterName   string `json:"cluster_name"`
	ClusterStatus string `json:"cluster_status"`
}

type GetClusterDetailsResponse struct {
	ClusterUUID                  string      `json:"cluster_uuid"`
	ClusterName                  string      `json:"cluster_name"`
	ClusterVersion               string      `json:"cluster_version"`
	ClusterStatus                string      `json:"cluster_status"`
	ClusterProjectUUID           string      `json:"cluster_project_uuid"`
	ClusterLoadbalancerUUID      string      `json:"cluster_loadbalancer_uuid"`
	ClusterMasterServerGroup     NodeGroup   `json:"cluster_master_server_group_uuid"`
	ClusterWorkerServerGroups    []NodeGroup `json:"cluster_worker_server_groups_uuid"`
	ClusterSubnets               []string    `json:"cluster_subnets"`
	ClusterEndpoint              string      `json:"cluster_endpoint"`
	ClusterAPIAccess             string      `json:"cluster_api_access"`
	ClusterCertificateExpireDate time.Time   `json:"cluster_certificate_expire_date"`
}

type GetClusterResponse struct {
	ClusterName                  string    `json:"clusterName"`
	ClusterID                    string    `json:"clusterId"`
	ProjectID                    string    `json:"projectId"`
	KubernetesVersion            string    `json:"kubernetesVersion"`
	ClusterAPIAccess             string    `json:"clusterApiAccess"`
	ClusterStatus                string    `json:"clusterStatus"`
	ClusterSharedSecurityGroup   string    `json:"clusterSharedSecurityGroup"`
	ClusterCertificateExpireDate time.Time `json:"cluster_certificate_expire_date"`
}

type DestroyCluster struct {
	ClusterID         string    `json:"cluster_id"`
	ClusterDeleteDate time.Time `json:"cluster_delete_date"`
	ClusterStatus     string    `json:"cluster_status"`
}

type GetKubeConfigResponse struct {
	ClusterUUID string `json:"cluster_uuid"`
	KubeConfig  string `json:"kubeconfig"`
}

type CreateKubeconfigResponse struct {
	ClusterUUID string `json:"cluster_uuid"`
}

type UpdateKubeconfigResponse struct {
	ClusterUUID string `json:"cluster_uuid"`
}

type UpdateClusterResponse struct {
	ClusterUUID string `json:"cluster_uuid"`
}
