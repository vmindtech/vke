package request

type UpdateNodeGroupRequest struct {
	MinNodes *uint32 `json:"minNodes,omitempty"`
	MaxNodes *uint32 `json:"maxNodes,omitempty"`

	Autoscale *bool `json:"autoscale,omitempty"`

	NodesToRemove []string `json:"nodesToRemove,omitempty"`
}

type CreateNodeGroupRequest struct {
	NodeGroupName    string   `json:"nodeGroupName"`
	NodeFlavorUUID   string   `json:"nodeFlavorUUID"`
	NodeDiskSize     int      `json:"nodeDiskSize"`
	NodeGroupLabels  []string `json:"nodeGroupLabels"`
	NodeGroupMinSize int      `json:"nodeGroupMinSize"`
	NodeGroupMaxSize int      `json:"nodeGroupMaxSize"`
}
