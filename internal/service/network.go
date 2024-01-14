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

type INetworkService interface {
	ListSubnetByName(ctx context.Context, subnetName, authToken string) (resource.ListSubnetByNameResponse, error)
	GetNetworkID(ctx context.Context, authToken, subnetID string) (resource.GetNetworkIdResponse, error)
	CreateSecurityGroup(ctx context.Context, authToken string, req request.CreateSecurityGroupRequest) (resource.CreateSecurityGroupResponse, error)
	CreateNetworkPort(ctx context.Context, authToken string, req request.CreateNetworkPortRequest) (resource.CreateNetworkPortResponse, error)
	CreateSecurityGroupRuleForIP(ctx context.Context, authToken string, req request.CreateSecurityGroupRuleForIpRequest) error
	CreateSecurityGroupRuleForSG(ctx context.Context, authToken string, req request.CreateSecurityGroupRuleForSgRequest) error
	CreateFloatingIP(ctx context.Context, authToken string, req request.CreateFloatingIPRequest) (resource.CreateFloatingIPResponse, error)
	DeleteSecurityGroup(ctx context.Context, authToken, clusterSecurityGroupId string) error
	DeleteFloatingIP(ctx context.Context, authToken, floatingIPID string) error
	GetSecurityGroupByID(ctx context.Context, authToken, securityGroupID string) (resource.GetSecurityGroupResponse, error)
	GetSubnetByID(ctx context.Context, authToken, subnetID string) (resource.SubnetResponse, error)
	GetComputePortId(ctx context.Context, authToken, computeID string) (resource.GetComputePortIdResponse, error)
}

type networkService struct {
	logger *logrus.Logger
}

func NewNetworkService(logger *logrus.Logger) INetworkService {
	return &networkService{
		logger: logger,
	}
}

func (ns *networkService) ListSubnetByName(ctx context.Context, subnetName, authToken string) (resource.ListSubnetByNameResponse, error) {
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s?name=%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, subnetsPath, subnetName), nil)
	if err != nil {
		ns.logger.Errorf("failed to create request, error: %v", err)
		return resource.ListSubnetByNameResponse{}, err
	}
	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		ns.logger.Errorf("failed to send request, error: %v", err)
		return resource.ListSubnetByNameResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		ns.logger.Errorf("failed to list subnet, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return resource.ListSubnetByNameResponse{}, fmt.Errorf("failed to list subnet, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.ListSubnetByNameResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		ns.logger.Errorf("failed to decode response, error: %v", err)
		return resource.ListSubnetByNameResponse{}, err
	}

	return respDecoder, nil
}

func (ns *networkService) GetNetworkID(ctx context.Context, authToken, subnetID string) (resource.GetNetworkIdResponse, error) {
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, subnetsPath, subnetID), nil)
	if err != nil {
		ns.logger.Errorf("failed to create request, error: %v", err)
		return resource.GetNetworkIdResponse{}, err
	}
	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		ns.logger.Errorf("failed to send request, error: %v", err)
		return resource.GetNetworkIdResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		ns.logger.Errorf("failed to list subnet, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return resource.GetNetworkIdResponse{}, fmt.Errorf("failed to list subnet, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.GetNetworkIdResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		ns.logger.Errorf("failed to decode response, error: %v", err)
		return resource.GetNetworkIdResponse{}, err
	}

	return respDecoder, nil
}

func (ns *networkService) CreateSecurityGroup(ctx context.Context, authToken string, req request.CreateSecurityGroupRequest) (resource.CreateSecurityGroupResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		ns.logger.Errorf("failed to marshal request, error: %v", err)
		return resource.CreateSecurityGroupResponse{}, err
	}
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, securityGroupPath), bytes.NewBuffer(data))
	if err != nil {
		ns.logger.Errorf("failed to create request, error: %v", err)
		return resource.CreateSecurityGroupResponse{}, err
	}
	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		ns.logger.Errorf("failed to send request, error: %v", err)
		return resource.CreateSecurityGroupResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		ns.logger.Errorf("failed to create security group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return resource.CreateSecurityGroupResponse{}, fmt.Errorf("failed to create security group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.CreateSecurityGroupResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		ns.logger.Errorf("failed to decode response, error: %v", err)
		return resource.CreateSecurityGroupResponse{}, err
	}

	return respDecoder, nil
}

func (ns *networkService) CreateNetworkPort(ctx context.Context, authToken string, req request.CreateNetworkPortRequest) (resource.CreateNetworkPortResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		ns.logger.Errorf("failed to marshal request, error: %v", err)
		return resource.CreateNetworkPortResponse{}, err
	}
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, networkPort), bytes.NewBuffer(data))
	if err != nil {
		ns.logger.Errorf("failed to create request, error: %v", err)
		return resource.CreateNetworkPortResponse{}, err
	}
	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		ns.logger.Errorf("failed to send request, error: %v", err)
		return resource.CreateNetworkPortResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		ns.logger.Errorf("failed to create network port, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return resource.CreateNetworkPortResponse{}, fmt.Errorf("failed to create network port, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.CreateNetworkPortResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		ns.logger.Errorf("failed to decode response, error: %v", err)
		return resource.CreateNetworkPortResponse{}, err
	}

	return respDecoder, nil
}

func (ns *networkService) CreateSecurityGroupRuleForIP(ctx context.Context, authToken string, req request.CreateSecurityGroupRuleForIpRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		ns.logger.Errorf("failed to marshal request, error: %v", err)
		return err
	}
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, SecurityGroupRulesPath), bytes.NewBuffer(data))
	if err != nil {
		ns.logger.Errorf("failed to create request, error: %v", err)
		return err
	}
	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		ns.logger.Errorf("failed to send request, error: %v", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, err := httputil.DumpResponse(resp, true)
		if err != nil {
			log.Fatalln(err)
		}

		ns.logger.Errorf("failed to create security group rule, status code: %v, error msg: %v", resp.StatusCode, string(b))
		return fmt.Errorf("failed to create security group rule, status code: %v, error msg: %v", resp.StatusCode, string(b))
	}
	return nil
}

func (ns *networkService) CreateSecurityGroupRuleForSG(ctx context.Context, authToken string, req request.CreateSecurityGroupRuleForSgRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		ns.logger.Errorf("failed to marshal request, error: %v", err)
		return err
	}
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, SecurityGroupRulesPath), bytes.NewBuffer(data))
	if err != nil {
		ns.logger.Errorf("failed to create request, error: %v", err)
		return err
	}
	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		ns.logger.Errorf("failed to send request, error: %v", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, err := httputil.DumpResponse(resp, true)
		if err != nil {
			log.Fatalln(err)
		}

		ns.logger.Errorf("failed to create security group rule, status code: %v, error msg: %v", resp.StatusCode, string(b))
		return fmt.Errorf("failed to create security group rule, status code: %v, error msg: %v", resp.StatusCode, string(b))
	}
	return nil
}

func (ns *networkService) CreateFloatingIP(ctx context.Context, authToken string, req request.CreateFloatingIPRequest) (resource.CreateFloatingIPResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		ns.logger.Errorf("failed to marshal request, error: %v", err)
		return resource.CreateFloatingIPResponse{}, err
	}
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, floatingIPPath), bytes.NewBuffer(data))
	if err != nil {
		ns.logger.Errorf("failed to create request, error: %v", err)
		return resource.CreateFloatingIPResponse{}, err
	}

	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		ns.logger.Errorf("failed to send request, error: %v", err)
		return resource.CreateFloatingIPResponse{}, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		ns.logger.Errorf("failed to create floating ip, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		b, err := httputil.DumpResponse(resp, true)
		if err != nil {
			log.Fatalln(err)
		}
		return resource.CreateFloatingIPResponse{}, fmt.Errorf("failed to create floating ip, status code: %v, error msg: %v, full msg: %v", resp.StatusCode, resp.Status, string(b))
	}

	var respDecoder resource.CreateFloatingIPResponse
	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		ns.logger.Errorf("failed to decode response, error: %v", err)
		return resource.CreateFloatingIPResponse{}, err
	}
	return respDecoder, nil
}

func (ns *networkService) DeleteSecurityGroup(ctx context.Context, authToken, clusterSecurityGroupId string) error {
	r, err := http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, securityGroupPath, clusterSecurityGroupId), nil)
	if err != nil {
		ns.logger.Errorf("failed to create request, error: %v", err)
		return err
	}

	r.Header.Add("X-Auth-Token", authToken)

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		ns.logger.Errorf("failed to send request, error: %v", err)
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("failed to delete security group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	return nil
}

func (ns *networkService) DeleteFloatingIP(ctx context.Context, authToken, floatingIPID string) error {
	r, err := http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, floatingIPPath, floatingIPID), nil)
	if err != nil {
		ns.logger.Errorf("failed to create request, error: %v", err)
		return err
	}

	r.Header.Add("X-Auth-Token", authToken)

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		ns.logger.Errorf("failed to send request, error: %v", err)
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("failed to delete floating ip, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}
	return nil
}

func (ns *networkService) GetSecurityGroupByID(ctx context.Context, authToken, securityGroupID string) (resource.GetSecurityGroupResponse, error) {
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, securityGroupPath, securityGroupID), nil)
	if err != nil {
		ns.logger.Errorf("failed to create request, error: %v", err)
		return resource.GetSecurityGroupResponse{}, err
	}

	r.Header.Add("X-Auth-Token", authToken)

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		ns.logger.Errorf("failed to send request, error: %v", err)
		return resource.GetSecurityGroupResponse{}, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		ns.logger.Errorf("failed to list security group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return resource.GetSecurityGroupResponse{}, fmt.Errorf("failed to list security group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		ns.logger.Errorf("failed to read response body, error: %v", err)
		return resource.GetSecurityGroupResponse{}, err
	}
	var respData resource.GetSecurityGroupResponse
	err = json.Unmarshal([]byte(body), &respData)
	if err != nil {
		ns.logger.Errorf("failed to unmarshal response body, error: %v", err)
		return resource.GetSecurityGroupResponse{}, err
	}

	return respData, nil
}

func (ns *networkService) GetSubnetByID(ctx context.Context, authToken, subnetID string) (resource.SubnetResponse, error) {
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, subnetsPath, subnetID), nil)
	if err != nil {
		ns.logger.Errorf("failed to create request, error: %v", err)
		return resource.SubnetResponse{}, err
	}

	r.Header.Add("X-Auth-Token", authToken)

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		ns.logger.Errorf("failed to send request, error: %v", err)
		return resource.SubnetResponse{}, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		ns.logger.Errorf("failed to list subnet, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return resource.SubnetResponse{}, fmt.Errorf("failed to list subnet, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		ns.logger.Errorf("failed to read response body, error: %v", err)
		return resource.SubnetResponse{}, err
	}
	var respData resource.SubnetResponse
	err = json.Unmarshal([]byte(body), &respData)
	if err != nil {
		ns.logger.Errorf("failed to unmarshal response body, error: %v", err)
		return resource.SubnetResponse{}, err
	}

	return respData, nil
}

func (cs *networkService) GetComputePortId(ctx context.Context, authToken, computeID string) (resource.GetComputePortIdResponse, error) {
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, computePath, computeID, osInterfacePath), nil)
	if err != nil {
		cs.logger.Errorf("failed to create request, error: %v", err)
		return resource.GetComputePortIdResponse{}, err
	}

	r.Header.Add("X-Auth-Token", authToken)

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		cs.logger.Errorf("failed to send request, error: %v", err)
		return resource.GetComputePortIdResponse{}, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		cs.logger.Errorf("failed to list interface, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return resource.GetComputePortIdResponse{}, fmt.Errorf("failed to list interface, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		cs.logger.Errorf("failed to read response body, error: %v", err)
		return resource.GetComputePortIdResponse{}, err
	}
	var respData map[string][]map[string]interface{}
	err = json.Unmarshal([]byte(body), &respData)
	if err != nil {
		cs.logger.Errorf("failed to unmarshal response body, error: %v", err)
		return resource.GetComputePortIdResponse{}, err
	}

	attachments := respData["interfaceAttachments"]

	portId := attachments[0]["port_id"].(string)

	respPort := resource.GetComputePortIdResponse{
		PortId: portId,
	}

	return respPort, nil
}
