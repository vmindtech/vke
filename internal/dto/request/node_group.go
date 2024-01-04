package request

type AddNodeRequest struct {
	ClusterID   string `json:"clusterId"`
	NodeGroupID string `json:"nodeGroupId"`
}
