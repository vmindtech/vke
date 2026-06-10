package request

type JobMessage struct {
	JobUUID string `json:"job_uuid"`
}

type CreateClusterJobPayload struct {
	Request   CreateClusterRequest `json:"request"`
	ClusterUUID string             `json:"cluster_uuid"`
}

type DeleteClusterJobPayload struct {
	// AuthToken is kept only for jobs enqueued by older builds; new jobs carry AuthTokenEnc (AES-GCM, VKE_ENCRYPTION_KEY).
	AuthToken    string `json:"auth_token,omitempty"`
	AuthTokenEnc string `json:"auth_token_enc,omitempty"`
	ClusterID    string `json:"cluster_id"`
}

