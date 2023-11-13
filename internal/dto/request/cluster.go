package request

type CreateClusterRequest struct {
	ClusterName            string   `json:"clusterName"`
	ProjectID              string   `json:"projectId"`
	KubernetesVersion      string   `json:"kubernetesVersion"`
	NodeKeyPairName        string   `json:"nodeKeyPairName"`
	ClusterAPIAccess       string   `json:"clusterApiAccess"`
	SubnetIDs              []string `json:"subnetIds"`
	WorkerCount            int      `json:"workerCount"`
	WorkerInstanceFlavorID string   `json:"workerInstanceFlavorID"`
	MasterInstanceFlavorID string   `json:"masterInstanceFlavorID"`
}

type CreateComputeRequest struct {
	Server Server `json:"server"`
}

type Server struct {
	Name                 string                 `json:"name"`
	ImageRef             string                 `json:"imageRef"`
	FlavorRef            string                 `json:"flavorRef"`
	KeyName              string                 `json:"key_name"`
	AvailabilityZone     string                 `json:"availability_zone"`
	SecurityGroups       []SecurityGroups       `json:"security_groups"`
	BlockDeviceMappingV2 []BlockDeviceMappingV2 `json:"block_device_mapping_v2"`
	Networks             []Networks             `json:"networks"`
	UserData             string                 `json:"user_data"`
}

type BlockDeviceMappingV2 struct {
	BootIndex           int    `json:"boot_index"`
	UUID                string `json:"uuid"`
	SourceType          string `json:"source_type"`
	DestinationType     string `json:"destination_type"`
	DeleteOnTermination bool   `json:"delete_on_termination"`
}

type Networks struct {
	UUID string `json:"uuid"`
}

type SecurityGroups struct {
	Name string `json:"name"`
}
