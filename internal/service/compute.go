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
)

type IComputeService interface {
	CreateCompute(ctx context.Context, authToken string, req request.CreateComputeRequest) (resource.CreateComputeResponse, error)
	CreateServerGroup(ctx context.Context, authToken string, req request.CreateServerGroupRequest) (resource.ServerGroupResponse, error)
	DeleteServerGroup(ctx context.Context, authToken, clusterMasterServerGroupUUID string, clusterWorkerServerGroupsUUID []string) error
	DeleteComputeandPort(ctx context.Context, authToken, serverID, clusterMasterServerGroupUUID string, clusterWorkerGroupsUUID []string) error
	GetCountOfServerFromServerGroup(ctx context.Context, authToken, serverGroupID string) (int, error)
}

type computeService struct {
	logger *logrus.Logger
}

func NewComputeService(l *logrus.Logger) IComputeService {
	return &computeService{
		logger: l,
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

func (cs *computeService) DeleteServerGroup(ctx context.Context, authToken, clusterMasterServerGroupUUID string, clusterWorkerServerGroupsUUID []string) error {
	r, err := http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, serverGroupPath, clusterMasterServerGroupUUID), nil)
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

	for _, member := range clusterWorkerServerGroupsUUID {
		r, err = http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, serverGroupPath, member), nil)
		if err != nil {
			cs.logger.Errorf("failed to create request, error: %v", err)
			return err
		}

		r.Header.Add("X-Auth-Token", authToken)

		client = &http.Client{}
		resp, err = client.Do(r)
		if err != nil {
			cs.logger.Errorf("failed to send request, error: %v", err)
			return err
		}

		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNoContent {
			cs.logger.Errorf("failed to delete server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
			return fmt.Errorf("failed to delete server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		}

	}

	return nil
}

func (cs *computeService) DeleteComputeandPort(ctx context.Context, authToken, serverID, clusterMasterServerGroupUUID string, clusterWorkerGroupsUUID []string) error {
	for _, member := range clusterWorkerGroupsUUID {
		r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, serverGroupPath, member), nil)
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

		if resp.StatusCode != http.StatusOK {
			cs.logger.Errorf("failed to list server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
			return fmt.Errorf("failed to list server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			cs.logger.Errorf("failed to read response body, error: %v", err)
			return err
		}
		var respData map[string]map[string]interface{}
		err = json.Unmarshal([]byte(body), &respData)

		if err != nil {
			cs.logger.Errorf("failed to unmarshal response body, error: %v", err)
			return err
		}

		serverGroup := respData["server_group"]

		membersInterface := serverGroup["members"]

		members := membersInterface.([]interface{})

		for _, instance := range members {
			r, err = http.NewRequest("GET", fmt.Sprintf("%s/%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, computePath, instance, osInterfacePath), nil)
			if err != nil {
				cs.logger.Errorf("failed to create request, error: %v", err)
				return err
			}

			r.Header.Add("X-Auth-Token", authToken)

			client = &http.Client{}
			resp, err = client.Do(r)
			if err != nil {
				cs.logger.Errorf("failed to send request, error: %v", err)
				return err
			}

			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				cs.logger.Errorf("failed to list interface, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
				return fmt.Errorf("failed to list interface, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
			}

			body, err := io.ReadAll(resp.Body)
			if err != nil {
				cs.logger.Errorf("failed to read response body, error: %v", err)
				return err
			}
			var respData map[string][]map[string]interface{}
			err = json.Unmarshal([]byte(body), &respData)
			if err != nil {
				cs.logger.Errorf("failed to unmarshal response body, error: %v", err)
				return err
			}

			attachments := respData["interfaceAttachments"]

			portID := attachments[0]["port_id"].(string)

			r, err = http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, networkPort, portID), nil)
			if err != nil {
				cs.logger.Errorf("failed to create request, error: %v", err)
				return err
			}

			r.Header.Add("X-Auth-Token", authToken)

			client = &http.Client{}
			resp, err = client.Do(r)
			if err != nil {
				cs.logger.Errorf("failed to send request, error: %v", err)
				return err
			}

			defer resp.Body.Close()

			if resp.StatusCode != http.StatusNoContent {
				cs.logger.Errorf("failed to delete port, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
				return fmt.Errorf("failed to delete port, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
			}

			r, err = http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, computePath, instance), nil)
			if err != nil {
				cs.logger.Errorf("failed to create request, error: %v", err)
				return err
			}

			r.Header.Add("X-Auth-Token", authToken)
			r.Header.Add("Content-Type", "application/json")

			client = &http.Client{}
			resp, err = client.Do(r)
			if err != nil {
				cs.logger.Errorf("failed to send request, error: %v", err)
				return err
			}

			defer resp.Body.Close()

			if resp.StatusCode != http.StatusNoContent {
				cs.logger.Errorf("failed to delete compute, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
				return fmt.Errorf("failed to delete compute, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
			}
		}
	}

	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, serverGroupPath, clusterMasterServerGroupUUID), nil)
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

	if resp.StatusCode != http.StatusOK {
		cs.logger.Errorf("failed to list server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return fmt.Errorf("failed to list server group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		cs.logger.Errorf("failed to read response body, error: %v", err)
		return err
	}
	var respData map[string]map[string]interface{}
	err = json.Unmarshal([]byte(body), &respData)
	if err != nil {
		cs.logger.Errorf("failed to unmarshal response body, error: %v", err)
		return err
	}

	serverGroup := respData["server_group"]

	membersInterface := serverGroup["members"]

	members := membersInterface.([]interface{})

	for _, instance := range members {
		r, err = http.NewRequest("GET", fmt.Sprintf("%s/%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, computePath, instance, osInterfacePath), nil)
		if err != nil {
			cs.logger.Errorf("failed to create request, error: %v", err)
			return err
		}

		r.Header.Add("X-Auth-Token", authToken)

		client = &http.Client{}
		resp, err = client.Do(r)
		if err != nil {
			cs.logger.Errorf("failed to send request, error: %v", err)
			return err
		}

		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			cs.logger.Errorf("failed to list interface, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
			return fmt.Errorf("failed to list interface, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			cs.logger.Errorf("failed to read response body, error: %v", err)
			return err
		}
		var respData map[string][]map[string]interface{}
		err = json.Unmarshal([]byte(body), &respData)
		if err != nil {
			cs.logger.Errorf("failed to unmarshal response body, error: %v", err)
			return err
		}

		attachments := respData["interfaceAttachments"]

		portID := attachments[0]["port_id"].(string)

		r, err = http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, networkPort, portID), nil)
		if err != nil {
			cs.logger.Errorf("failed to create request, error: %v", err)
			return err
		}

		r.Header.Add("X-Auth-Token", authToken)

		client = &http.Client{}
		resp, err = client.Do(r)
		if err != nil {
			cs.logger.Errorf("failed to send request, error: %v", err)
			return err
		}

		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNoContent {
			cs.logger.Errorf("failed to delete port, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
			return fmt.Errorf("failed to delete port, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		}

		r, err = http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, computePath, instance), nil)
		if err != nil {
			cs.logger.Errorf("failed to create request, error: %v", err)
			return err
		}

		r.Header.Add("X-Auth-Token", authToken)
		r.Header.Add("Content-Type", "application/json")

		client = &http.Client{}
		resp, err = client.Do(r)
		if err != nil {
			cs.logger.Errorf("failed to send request, error: %v", err)
			return err
		}

		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNoContent {
			cs.logger.Errorf("failed to delete compute, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
			return fmt.Errorf("failed to delete compute, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		}
	}
	return nil
}

func (cs *computeService) GetCountOfServerFromServerGroup(ctx context.Context, authToken, serverGroupID string) (int, error) {
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
