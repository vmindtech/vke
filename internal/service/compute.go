package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"

	"github.com/sirupsen/logrus"
	"github.com/vmindtech/vke/config"
	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/internal/repository"
)

type IComputeService interface {
	CreateCompute(ctx context.Context, authToken string, req request.CreateComputeRequest) (resource.CreateComputeResponse, error)
	CreateServerGroup(ctx context.Context, authToken string, req request.CreateServerGroupRequest) (resource.ServerGroupResponse, error)
	DeleteServerGroup(ctx context.Context, authToken, clusterMasterServerGroupUUID string, clusterWorkerServerGroupsUUID []string) error
	DeleteComputeandPort(ctx context.Context, authToken, serverID, clusterMasterServerGroupUUID string, clusterWorkerGroupsUUID []string) error
	GetCountOfServerFromServerGroup(ctx context.Context, authToken, serverGroupID, projectUUID string) (int, error)
	GetInstances(ctx context.Context, authToken, nodeGroupUUID string) ([]resource.Servers, error)
	GetClusterFlavor(ctx context.Context, authToken string, clusterUUID string) ([]resource.Flavor, error)
	DeleteCompute(ctx context.Context, authToken, serverID string) error
}

type computeService struct {
	logger          *logrus.Logger
	identityService IIdentityService
	repository      repository.IRepository
}

func NewComputeService(l *logrus.Logger, i IIdentityService, repository repository.IRepository) IComputeService {
	return &computeService{
		logger:          l,
		identityService: i,
		repository:      repository,
	}
}

func (cs *computeService) CreateCompute(ctx context.Context, authToken string, req request.CreateComputeRequest) (resource.CreateComputeResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return resource.CreateComputeResponse{}, err
	}
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, computePath), bytes.NewBuffer(data))
	if err != nil {
		return resource.CreateComputeResponse{}, err
	}
	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		return resource.CreateComputeResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		b, err := httputil.DumpResponse(resp, true)
		if err != nil {
			log.Fatalln(err)
		}
		cs.logger.Errorf("failed to create compute, status code: %v, error msg: %v full: %v", resp.StatusCode, resp.Status, string(b))

		return resource.CreateComputeResponse{}, fmt.Errorf("failed to create compute, status code: %v, error msg: %v", resp.StatusCode, string(b))
	}

	var respDecoder resource.CreateComputeResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		return resource.CreateComputeResponse{}, err
	}

	return respDecoder, nil
}

func (cs *computeService) CreateServerGroup(ctx context.Context, authToken string, req request.CreateServerGroupRequest) (resource.ServerGroupResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		cs.logger.Errorf("failed to marshal request, error: %v", err)
		return resource.ServerGroupResponse{}, err
	}

	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, serverGroupPath), bytes.NewBuffer(data))
	if err != nil {
		cs.logger.Errorf("failed to create request, error: %v", err)
		return resource.ServerGroupResponse{}, err
	}

	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")
	r.Header.Add("x-openstack-nova-api-version", config.GlobalConfig.GetOpenStackApiConfig().NovaMicroversion)

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		cs.logger.Errorf("failed to send request, error: %v", err)
		return resource.ServerGroupResponse{}, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		cs.logger.Errorf("failed to create server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return resource.ServerGroupResponse{}, fmt.Errorf("failed to create server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.ServerGroupResponse
	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		cs.logger.Errorf("failed to decode response, error: %v", err)
		return resource.ServerGroupResponse{}, err
	}
	return respDecoder, nil
}

func (cs *computeService) DeleteServerGroup(ctx context.Context, authToken, clusterServerGroupUUID string) error {
	r, err := http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, serverGroupPath, clusterServerGroupUUID), nil)
	if err != nil {
		cs.logger.Errorf("failed to create request, error: %v", err)
		return err
	}

	r.Header.Add("X-Auth-Token", authToken)

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		cs.logger.Errorf("failed to send request, error: %v", err)
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		cs.logger.Errorf("failed to delete server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return fmt.Errorf("failed to delete server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}
	return nil
}

func (cs *computeService) DeletePort(ctx context.Context, authToken, portID string) error {
	r, err := http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, networkPort, portID), nil)
	if err != nil {
		cs.logger.Errorf("failed to create request, error: %v", err)
		return err
	}

	r.Header.Add("X-Auth-Token", authToken)

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		cs.logger.Errorf("failed to send request, error: %v", err)
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		cs.logger.Errorf("failed to delete port, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return fmt.Errorf("failed to delete port, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}
	return nil
}
func (cs *computeService) DeleteCompute(ctx context.Context, authToken, serverID string) error {
	r, err := http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, computePath, serverID), nil)
	if err != nil {
		cs.logger.Errorf("failed to create request, error: %v", err)
		return err
	}

	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		cs.logger.Errorf("failed to send request, error: %v", err)
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		cs.logger.Errorf("failed to delete compute, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return fmt.Errorf("failed to delete compute, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}
	return nil
}

func (cs *computeService) GetServerGroupMemberList(ctx context.Context, authToken, ServerGroupID string) (resource.GetServerGroupMemberListResponse, error) {
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, serverGroupPath, ServerGroupID), nil)
	if err != nil {
		cs.logger.Errorf("failed to create request, error: %v", err)
		return resource.GetServerGroupMemberListResponse{}, err
	}

	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		cs.logger.Errorf("failed to send request, error: %v", err)
		return resource.GetServerGroupMemberListResponse{}, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		cs.logger.Errorf("failed to list server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return resource.GetServerGroupMemberListResponse{}, fmt.Errorf("failed to list server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		cs.logger.Errorf("failed to read response body, error: %v", err)
		return resource.GetServerGroupMemberListResponse{}, err
	}
	var respData map[string]map[string]interface{}
	err = json.Unmarshal([]byte(body), &respData)

	if err != nil {
		cs.logger.Errorf("failed to unmarshal response body, error: %v", err)
		return resource.GetServerGroupMemberListResponse{}, err
	}

	serverGroup := respData["server_group"]

	membersInterface := serverGroup["members"]

	members := membersInterface.([]interface{})

	var respMembers resource.GetServerGroupMemberListResponse

	for _, member := range members {
		respMembers.Members = append(respMembers.Members, member.(string))
	}

	return respMembers, nil
}
func (cs *computeService) DeleteCompute(ctx context.Context, authToken, serverID string) error {
	r, err := http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, computePath, serverID), nil)
	if err != nil {
		cs.logger.Errorf("failed to create request, error: %v", err)
		return err
	}

	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		cs.logger.Errorf("failed to send request, error: %v", err)
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		cs.logger.Errorf("failed to delete compute, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return fmt.Errorf("failed to delete compute, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	return nil
}
func (cs *computeService) GetCountOfServerFromServerGroup(ctx context.Context, authToken, serverGroupID, projectUUID string) (int, error) {
	err := cs.identityService.CheckAuthToken(ctx, authToken, projectUUID)
	if err != nil {
		cs.logger.Errorf("failed to check auth token, error: %v", err)
		return 0, err
	}
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, serverGroupPath, serverGroupID), nil)
	if err != nil {
		cs.logger.Errorf("failed to create request, error: %v", err)
		return 0, err
	}

	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		cs.logger.Errorf("failed to send request, error: %v", err)
		return 0, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		cs.logger.Errorf("failed to list server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return 0, fmt.Errorf("failed to list server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		cs.logger.Errorf("failed to read response body, error: %v", err)
		return 0, err
	}
	var respData resource.ServerGroupResponse
	err = json.Unmarshal([]byte(body), &respData)
	if err != nil {
		cs.logger.Errorf("failed to unmarshal response body, error: %v", err)
		return 0, err
	}

	return len(respData.ServerGroup.Members), nil
}
func (cs *computeService) GetInstances(ctx context.Context, authToken, nodeGroupUUID string) ([]resource.Servers, error) {
	ClusterUUID, err := cs.repository.NodeGroups().GetClusterProjectUUIDByNodeGroupUUID(ctx, nodeGroupUUID)
	if err != nil {
		return []resource.Servers{}, err
	}
	clusterProjectUUID, err := cs.repository.Cluster().GetClusterByUUID(ctx, ClusterUUID)
	if err != nil {
		return []resource.Servers{}, err
	}

	err = cs.identityService.CheckAuthToken(ctx, authToken, clusterProjectUUID.ClusterProjectUUID)
	if err != nil {
		cs.logger.Errorf("failed to check auth token, error: %v", err)
		return []resource.Servers{}, err
	}

	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, serverGroupPath, nodeGroupUUID), nil)
	if err != nil {
		cs.logger.Errorf("failed to create request, error: %v", err)
		return []resource.Servers{}, err
	}

	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		cs.logger.Errorf("failed to send request, error: %v", err)
		return []resource.Servers{}, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		cs.logger.Errorf("failed to list server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return []resource.Servers{}, fmt.Errorf("failed to list server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		cs.logger.Errorf("failed to read response body, error: %v", err)
		return []resource.Servers{}, err
	}
	var data resource.ServerGroupResponse
	err = json.Unmarshal([]byte(body), &data)
	if err != nil {
		cs.logger.Errorf("failed to unmarshal response body, error: %v", err)
		return []resource.Servers{}, err
	}
	// fmt.Println("ClusterUUID: ", ClusterUUID)
	// fmt.Println("NodeGroupUUID: ", nodeGroupUUID)
	// fmt.Println("authToken: ", authToken)

	nodeGroup, err := cs.repository.NodeGroups().GetNodeGroupByUUID(ctx, nodeGroupUUID)
	if err != nil {
		cs.logger.Errorf("failed to get node group, error: %v", err)
		return []resource.Servers{}, err
	}
	count, err := cs.GetCountOfServerFromServerGroup(ctx, authToken, nodeGroup.NodeGroupUUID, clusterProjectUUID.ClusterProjectUUID)
	if err != nil {
		cs.logger.Errorf("failed to check current node size, err: %v", err)
		return []resource.Servers{}, err
	}

	var respNodeGroup []resource.NodeGroup
	respNodeGroup = append(respNodeGroup, resource.NodeGroup{
		ClusterUUID:      nodeGroup.ClusterUUID,
		NodeGroupUUID:    nodeGroup.NodeGroupUUID,
		NodeGroupName:    nodeGroup.NodeGroupName,
		NodeGroupMinSize: nodeGroup.NodeGroupMinSize,
		NodeGroupMaxSize: nodeGroup.NodeGroupMaxSize,
		NodeDiskSize:     nodeGroup.NodeDiskSize,
		NodeFlavorUUID:   nodeGroup.NodeFlavorUUID,
		NodeGroupsType:   nodeGroup.NodeGroupsType,
		CurrentNodes:     count,
		NodeGroupsStatus: nodeGroup.NodeGroupsStatus,
	})
	var intanceDetail resource.OpenstacServersResponse
	var responseData []resource.Servers
	for _, member := range data.ServerGroup.Members {
		intanceDetail, err = cs.GetInstancesDetail(ctx, authToken, member)
		for _, data := range respNodeGroup {

			responseData = append(responseData, resource.Servers{
				ClusterUUID:   nodeGroup.ClusterUUID,
				InstanceName:  intanceDetail.OpenstackServers.Name,
				InstanceUUID:  intanceDetail.OpenstackServers.ID,
				NodeGroupUUID: nodeGroup.NodeGroupUUID,
				MinSize:       data.NodeGroupMinSize,
				MaxSize:       data.NodeGroupMaxSize,
				Flavor:        data.NodeFlavorUUID,
				Status:        intanceDetail.OpenstackServers.Status,
			})

		}
		if err != nil {
			cs.logger.Errorf("failed to get instance detail, error: %v", err)
			return []resource.Servers{}, err
		}
	}

	return responseData, nil
}
func (cs *computeService) GetInstancesDetail(ctx context.Context, authToken, instanceUUID string) (resource.OpenstacServersResponse, error) {
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, computePath, instanceUUID), nil)
	if err != nil {
		cs.logger.Errorf("failed to create request, error: %v", err)
		return resource.OpenstacServersResponse{}, err
	}
	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")
	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		cs.logger.Errorf("failed to send request, error: %v", err)
		return resource.OpenstacServersResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		cs.logger.Errorf("failed to list server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return resource.OpenstacServersResponse{}, fmt.Errorf("failed to list server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		cs.logger.Errorf("failed to read response body, error: %v", err)
		return resource.OpenstacServersResponse{}, err
	}
	var respData resource.OpenstacServersResponse
	err = json.Unmarshal([]byte(body), &respData)
	if err != nil {
		cs.logger.Errorf("failed to unmarshal response body, error: %v", err)
		return resource.OpenstacServersResponse{}, err
	}
	return respData, nil
}

func (cs *computeService) GetClusterFlavor(ctx context.Context, authToken string, clusterUUID string) ([]resource.Flavor, error) {
	clusterProjectUUID, err := cs.repository.Cluster().GetClusterByUUID(ctx, clusterUUID)
	if err != nil {
		cs.logger.Errorf("failed to get cluster project uuid by cluster uuid %s, err: %v", clusterUUID, err)
		return nil, err
	}
	err = cs.identityService.CheckAuthToken(ctx, authToken, clusterProjectUUID.ClusterProjectUUID)
	if err != nil {
		cs.logger.Errorf("failed to check auth token, err: %v", err)
		return nil, err
	}
	getNodeGroups, err := cs.repository.NodeGroups().GetNodeGroupsByClusterUUID(ctx, clusterUUID, "")
	if err != nil {
		cs.logger.Errorf("failed to get node groups by cluster uuid %s, err: %v", clusterUUID, err)
		return nil, err
	}
	var getFlavorsCluster []resource.Flavor
	for _, nodeGroup := range getNodeGroups {

		r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, flavorPath, nodeGroup.NodeFlavorUUID), nil)
		if err != nil {
			cs.logger.Errorf("failed to create request, error: %v", err)
			return nil, err
		}
		r.Header.Add("X-Auth-Token", authToken)
		r.Header.Add("Content-Type", "application/json")
		client := &http.Client{}
		resp, err := client.Do(r)
		if err != nil {
			cs.logger.Errorf("failed to send request, error: %v", err)
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			cs.logger.Errorf("failed to list server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
			return nil, fmt.Errorf("failed to list server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			cs.logger.Errorf("failed to read response body, error: %v", err)
			return nil, err
		}
		var respData resource.OpenstackFlavorResponse
		err = json.Unmarshal([]byte(body), &respData)
		if err != nil {
			cs.logger.Errorf("failed to unmarshal response body, error: %v", err)
			return nil, err
		}
		getFlavorsCluster = append(getFlavorsCluster, resource.Flavor{
			Id:       respData.Flavor.ID,
			Category: "compute",
			State:    "available",
			VCPUs:    respData.Flavor.VCPUs,
			RAM:      respData.Flavor.RAM,
			GPUs:     0,
		})
	}
	return getFlavorsCluster, nil
}
