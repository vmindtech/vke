package resource

type ClusterInfoResource struct {
	ClusterName string `json:"cluster_name"`
	ClusterID   string `json:"cluster_id"`
}

type CreateClusterResponse struct {
	ClusterUUID                   string   `json:"cluster_uuid"`
	ClusterName                   string   `json:"cluster_name"`
	ClusterVersion                string   `json:"cluster_version"`
	ClusterStatus                 string   `json:"cluster_status"`
	ClusterProjectUUID            string   `json:"cluster_project_uuid"`
	ClusterLoadbalancerUUID       string   `json:"cluster_loadbalancer_uuid"`
	ClusterMasterServerGroupUUID  string   `json:"cluster_master_server_group_uuid"`
	ClusterWorkerServerGroupsUUID []string `json:"cluster_worker_server_groups_uuid"`
	ClusterSubnets                []string `json:"cluster_subnets"`
	WorkerCount                   int      `json:"worker_count"`
	WorkerType                    string   `json:"worker_type"`
	WorkerDiskSize                int      `json:"worker_disk_size"`
	ClusterEndpoint               string   `json:"cluster_endpoint"`
	MasterSecurityGroup           string   `json:"master_security_group"`
	WorkerSecurityGroup           string   `json:"worker_security_group"`
	ClusterAPIAccess              string   `json:"cluster_api_access"`
}

type GetClusterResponse struct {
	ClusterID                     string   `json:"clusterId"`
	ProjectID                     string   `json:"projectId"`
	KubernetesVersion             string   `json:"kubernetesVersion"`
	ClusterAPIAccess              string   `json:"clusterApiAccess"`
	ClusterWorkerServerGroupsUUID []string `json:"clusterWorkerServerGroupsUUID"`
	ClusterStatus                 string   `json:"clusterStatus"`
}
