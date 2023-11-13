package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

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

func (a *clusterService) CreateCluster(ctx context.Context, authToken string, req request.CreateClusterRequest) (resource.CreateClusterResponse, error) {

	masterRequest := &request.CreateComputeRequest{
		Server: request.Server{
			Name:             "ServerName",
			ImageRef:         "ae5f8cea-303c-4093-89fc-934c946d5012",
			FlavorRef:        "29542b1f-0b0f-4fe3-93d9-398916a4dd67",
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
				},
			},
			Networks: []request.Networks{
				{Port: "33269d7d-132a-4589-9da2-79c8c5696a91"},
			},
			UserData: "IyEvYmluL2Jhc2gKY3VybCAtTE8gaHR0cHM6Ly90ci1pc3QtMDEtczMucG9ydHZtaW5kLmNvbS50ci9zd2lmdC92MS92a2UtaW5pdC92a2UtYWdlbnQKY3VybCAtTE8gaHR0cHM6Ly90ci1pc3QtMDEtczMucG9ydHZtaW5kLmNvbS50ci9zd2lmdC92MS92a2UtaW5pdC9jb25maWcueWFtbApjaG1vZCAreCB2a2UtYWdlbnQKLi92a2UtYWdlbnQgCi4vdmtlLWFnZW50IC1pbml0aWFsaXplPXRydWUgLXJrZTJBZ2VudFR5cGU9InNlcnZlciIgLXJrZTJUb2tlbj0idGlOSTlzNjJPbjc3SDA1WTZ2c1d0VmtjWlc3ZWw3VGZVMno9Z3dSSnQiIC1zZXJ2ZXJBZGRyZXNzPSJ0ZXN0LWs4cy5zYWtsYS5tZSIgLWt1YmV2ZXJzaW9uPSJ2MS4yOC4yK3JrZTJyMSIgIC10bHNTYW49InRlc3QtazhzLnNha2xhLm1lIg==",
		},
	}

	masterRequest.Server.Name = fmt.Sprintf("%v-master-1", req.ClusterName)

	firstMasterResp, err := a.CreateCompute(ctx, authToken, *masterRequest)
	if err != nil {
		return resource.CreateClusterResponse{}, err
	}
	fmt.Println(firstMasterResp)

	return resource.CreateClusterResponse{
		ClusterID: "vke-test-cluster",
		ProjectID: "vke-test-project",
	}, nil
}
func (a *clusterService) CreateCompute(ctx context.Context, authToken string, req request.CreateComputeRequest) (resource.CreateComputeResponse, error) {
	r, err := http.NewRequest("POST", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, bytes.NewBuffer([]byte(fmt.Sprintf("%v", req))))
	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")
	if err != nil {
		return resource.CreateComputeResponse{}, err
	}
	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	var respDecoder resource.CreateComputeResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		return resource.CreateComputeResponse{}, err
	}

	return respDecoder, nil
}
