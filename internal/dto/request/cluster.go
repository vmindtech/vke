package request

type CreateClusterRequest struct {
	ClusterName        string   `json:"clusterName"`
	ProjectID          string   `json:"projectId"`
	KubernetesVersion  string   `json:"kubernetesVersion"`
	NodeKeyPairName    string   `json:"nodeKeyPairName"`
	ClusterAPIAccess   string   `json:"clusterApiAccess"`
	SubnetIDs          []string `json:"subnetIds"`
	WorkerCount        int      `json:"workerCount"`
	WorkerInstanceType string   `json:"workerInstanceType"`
	MasterInstanceType string   `json:"masterInstanceType"`
}

type CreateComputeRequest struct {
	Server struct {
		Name             string `json:"name"`
		ImageRef         string `json:"imageRef"`
		FlavorRef        string `json:"flavorRef"`
		KeyName          string `json:"key_name"`
		AvailabilityZone string `json:"availability_zone"`
		SecurityGroups   []struct {
			Name string `json:"name"`
		} `json:"security_groups"`
		BlockDeviceMappingV2 []struct {
			BootIndex           int    `json:"boot_index"`
			UUID                string `json:"uuid"`
			SourceType          string `json:"source_type"`
			DestinationType     string `json:"destination_type"`
			DeleteOnTermination bool   `json:"delete_on_termination"`
		} `json:"block_device_mapping_v2"`
		Networks []struct {
			UUID string `json:"uuid"`
		} `json:"networks"`
		UserData string `json:"user_data"`
	} `json:"server"`
}
