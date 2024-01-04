package resource

type AddNodeResponse struct {
	ClusterID   string `json:"clusterId"`
	NodeGroupID string `json:"nodeGroupId"`
	MinSize     int    `json:"minSize"`
	MaxSize     int    `json:"maxSize"`
	ComputeID   string `json:"computeId"`
}
