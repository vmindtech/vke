package request

type CreateComputeRequest struct {
	Server         Server         `json:"server"`
	SchedulerHints SchedulerHints `json:"os:scheduler_hints"`
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

type SchedulerHints struct {
	Group string `json:"group"`
}

type BlockDeviceMappingV2 struct {
	BootIndex           int    `json:"boot_index"`
	UUID                string `json:"uuid"`
	SourceType          string `json:"source_type"`
	DestinationType     string `json:"destination_type"`
	DeleteOnTermination bool   `json:"delete_on_termination"`
	VolumeSize          int    `json:"volume_size"`
}

type Networks struct {
	Port string `json:"port"`
}

type SecurityGroups struct {
	Name string `json:"name"`
}

type CreateServerGroupRequest struct {
	ServerGroup ServerGroup `json:"server_group"`
}

type ServerGroup struct {
	Name     string   `json:"name"`
	Policies []string `json:"policies"`
}
