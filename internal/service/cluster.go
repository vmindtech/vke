package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/k0kubun/pp"
	"github.com/sirupsen/logrus"
	"github.com/vmindtech/vke/config"
	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/internal/repository"
)

type IClusterService interface {
	CreateCluster(ctx context.Context, authToken string, req request.CreateClusterRequest) (resource.CreateClusterResponse, error)
	CreateCompute(ctx context.Context, authToken string, req request.CreateComputeRequest) (resource.CreateComputeResponse, error)
}

type clusterService struct {
	repository repository.IRepository
	logger     *logrus.Logger
}

func NewClusterService(l *logrus.Logger, r repository.IRepository) IClusterService {
	return &clusterService{
		repository: r,
		logger:     l,
	}
}

const (
	createComputePath = "servers"
)

func (a *clusterService) CreateCluster(ctx context.Context, authToken string, req request.CreateClusterRequest) (resource.CreateClusterResponse, error) {

	masterRequest := &request.CreateComputeRequest{
		Server: request.Server{
			Name:             "ServerName",
			ImageRef:         "ae5f8cea-303c-4093-89fc-934c946d5012",
			FlavorRef:        req.MasterInstanceFlavorID,
			KeyName:          req.NodeKeyPairName,
			AvailabilityZone: "nova",
			SecurityGroups: []request.SecurityGroups{
				{Name: "default"},
			},
			BlockDeviceMappingV2: []request.BlockDeviceMappingV2{
				{
					BootIndex:           0,
					DestinationType:     "volume",
					DeleteOnTermination: true,
					SourceType:          "image",
					UUID:                "ae5f8cea-303c-4093-89fc-934c946d5012",
					VolumeSize:          50,
				},
			},
			Networks: []request.Networks{
				{UUID: "33269d7d-132a-4589-9da2-79c8c5696a91"},
			},
			UserData: "IyEvYmluL2Jhc2gKY3VybCAtTE8gLWsgaHR0cHM6Ly90ci1pc3QtMDEtczMucG9ydHZtaW5kLmNvbS50ci9zd2lmdC92MS92a2UtaW5pdC92a2UtYWdlbnQKY3VybCAtTE8gLWsgIGh0dHBzOi8vdHItaXN0LTAxLXMzLnBvcnR2bWluZC5jb20udHIvc3dpZnQvdjEvdmtlLWluaXQvY29uZmlnLnlhbWwKY2htb2QgK3ggdmtlLWFnZW50Ci4vdmtlLWFnZW50IAouL3ZrZS1hZ2VudCAtaW5pdGlhbGl6ZT10cnVlIC1ya2UyQWdlbnRUeXBlPSJzZXJ2ZXIiIC1ya2UyVG9rZW49InRpTkk5czYyT243N0gwNVk2dnNXdFZrY1pXN2VsN1RmVTJ6PWd3Ukp0IiAtc2VydmVyQWRkcmVzcz0idGVzdC1rOHMuc2FrbGEubWUiIC1rdWJldmVyc2lvbj0idjEuMjguMitya2UycjEiICAtdGxzU2FuPSJ0ZXN0LWs4cy5zYWtsYS5tZSI=",
		},
	}

	masterRequest.Server.Name = fmt.Sprintf("%v-master-1", req.ClusterName)

	firstMasterResp, err := a.CreateCompute(ctx, authToken, *masterRequest)
	if err != nil {
		return resource.CreateClusterResponse{}, err
	}
	fmt.Println("firstMasterResp: ")
	fmt.Println(firstMasterResp)

	return resource.CreateClusterResponse{
		ClusterID: "vke-test-cluster",
		ProjectID: "vke-test-project",
	}, nil
}

func (a *clusterService) CreateCompute(ctx context.Context, authToken string, req request.CreateComputeRequest) (resource.CreateComputeResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return resource.CreateComputeResponse{}, err
	}
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, createComputePath), bytes.NewBuffer(data))
	if err != nil {
		return resource.CreateComputeResponse{}, err
	}
	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")
	pp.Print(req)
	if err != nil {
		return resource.CreateComputeResponse{}, err
	}
	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		return resource.CreateComputeResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		a.logger.Errorf("failed to create compute, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return resource.CreateComputeResponse{}, fmt.Errorf("failed to create compute, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.CreateComputeResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		return resource.CreateComputeResponse{}, err
	}

	return respDecoder, nil
}
