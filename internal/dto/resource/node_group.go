package resource

type AddNodeResponse struct {
	ClusterID   string `json:"clusterId"`
	NodeGroupID string `json:"nodeGroupId"`
	MinSize     int    `json:"minSize"`
	MaxSize     int    `json:"maxSize"`
	ComputeID   string `json:"computeId"`
}

type GetNodeGroupsResponse struct {
	NodeGroups []NodeGroup `json:"node_groups"`
}

type NodeGroup struct {
	ClusterUUID      string `json:"cluster_uuid"`
	NodeGroupUUID    string `json:"node_group_uuid"`
	NodeGroupName    string `json:"node_group_name"`
	NodeGroupMinSize int    `json:"node_group_min_size"`
	NodeGroupMaxSize int    `json:"node_group_max_size"`
	NodeDiskSize     int    `json:"node_disk_size"`
	NodeFlavorUUID   string `json:"node_flavor_uuid"`
	NodeGroupsType   string `json:"node_groups_type"`
}
