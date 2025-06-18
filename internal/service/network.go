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
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/vmindtech/vke/config"
	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/pkg/constants"
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
	DeleteNetworkPort(ctx context.Context, authToken string, portID string) error
	GetSecurityGroupByID(ctx context.Context, authToken, securityGroupID string) (resource.GetSecurityGroupResponse, error)
	GetSubnetByID(ctx context.Context, authToken, subnetID string) (resource.SubnetResponse, error)
	GetComputeNetworkPorts(ctx context.Context, authToken, instanceID string) (resource.NetworkPortsResponse, error)
	GetSecurityGroupPorts(ctx context.Context, authToken, securityGroupID string) (resource.NetworkPortsResponse, error)
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
	token := strings.Clone(authToken)
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s?name=%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, constants.SubnetsPath, subnetName), nil)
	if err != nil {
		ns.logger.WithError(err).Error("failed to create request")
		return resource.ListSubnetByNameResponse{}, err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		ns.logger.WithError(err).Error("failed to send request")
		return resource.ListSubnetByNameResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		ns.logger.WithFields(logrus.Fields{
			"status_code": resp.StatusCode,
			"error_msg":   resp.Status,
		}).Error("failed to list subnet")
		return resource.ListSubnetByNameResponse{}, fmt.Errorf("failed to list subnet, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.ListSubnetByNameResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		ns.logger.WithError(err).Error("failed to decode response")
		return resource.ListSubnetByNameResponse{}, err
	}

	return respDecoder, nil
}

func (ns *networkService) GetNetworkID(ctx context.Context, authToken, subnetID string) (resource.GetNetworkIdResponse, error) {
	token := strings.Clone(authToken)
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, constants.SubnetsPath, subnetID), nil)
	if err != nil {
		ns.logger.WithError(err).Error("failed to create request")
		return resource.GetNetworkIdResponse{}, err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		ns.logger.WithError(err).Error("failed to send request")
		return resource.GetNetworkIdResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		ns.logger.WithFields(logrus.Fields{
			"status_code": resp.StatusCode,
			"error_msg":   resp.Status,
		}).Error("failed to list subnet")
		return resource.GetNetworkIdResponse{}, fmt.Errorf("failed to list subnet, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.GetNetworkIdResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		ns.logger.WithError(err).Error("failed to decode response")
		return resource.GetNetworkIdResponse{}, err
	}

	return respDecoder, nil
}

func (ns *networkService) CreateSecurityGroup(ctx context.Context, authToken string, req request.CreateSecurityGroupRequest) (resource.CreateSecurityGroupResponse, error) {
	token := strings.Clone(authToken)
	data, err := json.Marshal(req)
	if err != nil {
		ns.logger.WithError(err).Error("failed to marshal request")
		return resource.CreateSecurityGroupResponse{}, err
	}
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, constants.SecurityGroupPath), bytes.NewBuffer(data))
	if err != nil {
		ns.logger.WithError(err).Error("failed to create request")
		return resource.CreateSecurityGroupResponse{}, err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		ns.logger.WithError(err).Error("failed to send request")
		return resource.CreateSecurityGroupResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		ns.logger.WithFields(logrus.Fields{
			"status_code": resp.StatusCode,
			"error_msg":   resp.Status,
		}).Error("failed to create security group")
		return resource.CreateSecurityGroupResponse{}, fmt.Errorf("failed to create security group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.CreateSecurityGroupResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		ns.logger.WithError(err).Error("failed to decode response")
		return resource.CreateSecurityGroupResponse{}, err
	}

	return respDecoder, nil
}

func (ns *networkService) CreateNetworkPort(ctx context.Context, authToken string, req request.CreateNetworkPortRequest) (resource.CreateNetworkPortResponse, error) {
	token := strings.Clone(authToken)
	data, err := json.Marshal(req)
	if err != nil {
		ns.logger.WithError(err).Error("failed to marshal request")
		return resource.CreateNetworkPortResponse{}, err
	}
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, constants.NetworkPort), bytes.NewBuffer(data))
	if err != nil {
		ns.logger.WithError(err).Error("failed to create request")
		return resource.CreateNetworkPortResponse{}, err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		ns.logger.WithError(err).Error("failed to send request")
		return resource.CreateNetworkPortResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		ns.logger.WithFields(logrus.Fields{
			"status_code": resp.StatusCode,
			"error_msg":   resp.Status,
		}).Error("failed to create network port")
		return resource.CreateNetworkPortResponse{}, fmt.Errorf("failed to create network port, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.CreateNetworkPortResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		ns.logger.WithError(err).Error("failed to decode response")
		return resource.CreateNetworkPortResponse{}, err
	}

	return respDecoder, nil
}

func (ns *networkService) CreateSecurityGroupRuleForIP(ctx context.Context, authToken string, req request.CreateSecurityGroupRuleForIpRequest) error {
	token := strings.Clone(authToken)
	data, err := json.Marshal(req)
	if err != nil {
		ns.logger.WithError(err).Error("failed to marshal request")
		return err
	}
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, constants.SecurityGroupRulesPath), bytes.NewBuffer(data))
	if err != nil {
		ns.logger.WithError(err).Error("failed to create request")
		return err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		ns.logger.WithError(err).Error("failed to send request")
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, err := httputil.DumpResponse(resp, true)
		if err != nil {
			log.Fatalln(err)
		}

		ns.logger.WithFields(logrus.Fields{
			"status_code": resp.StatusCode,
			"error_msg":   string(b),
		}).Error("failed to create security group rule")
		return fmt.Errorf("failed to create security group rule, status code: %v, error msg: %v", resp.StatusCode, string(b))
	}
	return nil
}

func (ns *networkService) CreateSecurityGroupRuleForSG(ctx context.Context, authToken string, req request.CreateSecurityGroupRuleForSgRequest) error {
	token := strings.Clone(authToken)
	data, err := json.Marshal(req)
	if err != nil {
		ns.logger.WithError(err).Error("failed to marshal request")
		return err
	}
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, constants.SecurityGroupRulesPath), bytes.NewBuffer(data))
	if err != nil {
		ns.logger.WithError(err).Error("failed to create request")
		return err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		ns.logger.WithError(err).Error("failed to send request")
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, err := httputil.DumpResponse(resp, true)
		if err != nil {
			log.Fatalln(err)
		}

		ns.logger.WithFields(logrus.Fields{
			"status_code": resp.StatusCode,
			"error_msg":   string(b),
		}).Error("failed to create security group rule")
		return fmt.Errorf("failed to create security group rule, status code: %v, error msg: %v", resp.StatusCode, string(b))
	}
	return nil
}

func (ns *networkService) CreateFloatingIP(ctx context.Context, authToken string, req request.CreateFloatingIPRequest) (resource.CreateFloatingIPResponse, error) {
	token := strings.Clone(authToken)
	data, err := json.Marshal(req)
	if err != nil {
		ns.logger.WithError(err).Error("failed to marshal request")
		return resource.CreateFloatingIPResponse{}, err
	}
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, constants.FloatingIPPath), bytes.NewBuffer(data))
	if err != nil {
		ns.logger.WithError(err).Error("failed to create request")
		return resource.CreateFloatingIPResponse{}, err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		ns.logger.WithError(err).Error("failed to send request")
		return resource.CreateFloatingIPResponse{}, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		ns.logger.WithFields(logrus.Fields{
			"status_code": resp.StatusCode,
			"error_msg":   resp.Status,
		}).Error("failed to create floating ip")
		b, err := httputil.DumpResponse(resp, true)
		if err != nil {
			log.Fatalln(err)
		}
		return resource.CreateFloatingIPResponse{}, fmt.Errorf("failed to create floating ip, status code: %v, error msg: %v, full msg: %v", resp.StatusCode, resp.Status, string(b))
	}

	var respDecoder resource.CreateFloatingIPResponse
	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		ns.logger.WithError(err).Error("failed to decode response")
		return resource.CreateFloatingIPResponse{}, err
	}
	return respDecoder, nil
}

func (ns *networkService) DeleteSecurityGroup(ctx context.Context, authToken, clusterSecurityGroupId string) error {
	token := strings.Clone(authToken)
	r, err := http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, constants.SecurityGroupPath, clusterSecurityGroupId), nil)
	if err != nil {
		ns.logger.WithError(err).Error("failed to create request")
		return err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		ns.logger.WithError(err).Error("failed to send request")
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("failed to delete %s security group, status code: %v, error msg: %v", clusterSecurityGroupId, resp.StatusCode, resp.Status)
	}

	return nil
}

func (ns *networkService) DeleteFloatingIP(ctx context.Context, authToken, floatingIPID string) error {
	token := strings.Clone(authToken)
	r, err := http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, constants.FloatingIPPath, floatingIPID), nil)
	if err != nil {
		ns.logger.WithError(err).Error("failed to create request")
		return err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		ns.logger.WithError(err).Error("failed to send request")
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("failed to delete floating ip, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}
	return nil
}

func (ns *networkService) GetSecurityGroupByID(ctx context.Context, authToken, securityGroupID string) (resource.GetSecurityGroupResponse, error) {
	token := strings.Clone(authToken)
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, constants.SecurityGroupPath, securityGroupID), nil)
	if err != nil {
		ns.logger.WithError(err).Error("failed to create request")
		return resource.GetSecurityGroupResponse{}, err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		ns.logger.WithError(err).Error("failed to send request")
		return resource.GetSecurityGroupResponse{}, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		ns.logger.WithFields(logrus.Fields{
			"status_code": resp.StatusCode,
			"error_msg":   resp.Status,
		}).Error("failed to list security group")
		return resource.GetSecurityGroupResponse{}, fmt.Errorf("failed to list security group, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		ns.logger.WithError(err).Error("failed to read response body")
		return resource.GetSecurityGroupResponse{}, err
	}
	var respData resource.GetSecurityGroupResponse
	err = json.Unmarshal([]byte(body), &respData)
	if err != nil {
		ns.logger.WithError(err).Error("failed to unmarshal response body")
		return resource.GetSecurityGroupResponse{}, err
	}

	return respData, nil
}

func (ns *networkService) GetSubnetByID(ctx context.Context, authToken, subnetID string) (resource.SubnetResponse, error) {
	token := strings.Clone(authToken)
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, constants.SubnetsPath, subnetID), nil)
	if err != nil {
		ns.logger.WithError(err).Error("failed to create request")
		return resource.SubnetResponse{}, err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		ns.logger.WithError(err).Error("failed to send request")
		return resource.SubnetResponse{}, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		ns.logger.WithFields(logrus.Fields{
			"status_code": resp.StatusCode,
			"error_msg":   resp.Status,
		}).Error("failed to list subnet")
		return resource.SubnetResponse{}, fmt.Errorf("failed to list subnet, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		ns.logger.WithError(err).Error("failed to read response body")
		return resource.SubnetResponse{}, err
	}
	var respData resource.SubnetResponse
	err = json.Unmarshal([]byte(body), &respData)
	if err != nil {
		ns.logger.WithError(err).Error("failed to unmarshal response body")
		return resource.SubnetResponse{}, err
	}

	return respData, nil
}

func (ns *networkService) GetComputeNetworkPorts(ctx context.Context, authToken, instanceID string) (resource.NetworkPortsResponse, error) {
	token := strings.Clone(authToken)
	getNetworkDetail, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().ComputeEndpoint, constants.ComputePath, instanceID, constants.OSInterfacePath), nil)
	if err != nil {
		ns.logger.WithError(err).Error("failed to create request")
		return resource.NetworkPortsResponse{}, err
	}
	getNetworkDetail.Header.Add("X-Auth-Token", token)

	client := &http.Client{}
	resp, err := client.Do(getNetworkDetail)
	if err != nil {
		ns.logger.WithError(err).Error("failed to send request")
		return resource.NetworkPortsResponse{}, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		ns.logger.WithError(err).Error("failed to list interface")
		return resource.NetworkPortsResponse{}, fmt.Errorf("failed to list interface, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		ns.logger.WithError(err).Error("failed to read response body")
		return resource.NetworkPortsResponse{}, err
	}
	var respData map[string][]map[string]interface{}
	err = json.Unmarshal([]byte(body), &respData)
	if err != nil {
		ns.logger.WithError(err).Error("failed to unmarshal response body")
		return resource.NetworkPortsResponse{}, err
	}

	attachments := respData["interfaceAttachments"]
	var portIDs resource.NetworkPortsResponse
	for _, attachment := range attachments {
		portIDs.Ports = append(portIDs.Ports, attachment["port_id"].(string))
	}
	return portIDs, nil
}

func (ns *networkService) DeleteNetworkPort(ctx context.Context, authToken string, portID string) error {
	token := strings.Clone(authToken)
	r, err := http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, constants.NetworkPort, portID), nil)
	if err != nil {
		ns.logger.WithError(err).Error("failed to create request")
		return err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		ns.logger.WithError(err).Error("failed to send request")
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		ns.logger.WithFields(logrus.Fields{
			"status_code": resp.StatusCode,
			"error_msg":   resp.Status,
		}).Error("failed to delete network port")
		return fmt.Errorf("failed to delete network port, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	return nil
}

func (ns *networkService) GetSecurityGroupPorts(ctx context.Context, authToken, securityGroupID string) (resource.NetworkPortsResponse, error) {
	token := strings.Clone(authToken)
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().NetworkEndpoint, constants.NetworkPort), nil)
	if err != nil {
		ns.logger.WithError(err).Error("failed to create request")
		return resource.NetworkPortsResponse{}, err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		ns.logger.WithError(err).Error("failed to send request")
		return resource.NetworkPortsResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		ns.logger.WithFields(logrus.Fields{
			"status_code": resp.StatusCode,
			"error_msg":   resp.Status,
		}).Error("failed to list ports")
		return resource.NetworkPortsResponse{}, fmt.Errorf("failed to list ports, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		ns.logger.WithError(err).Error("failed to read response body")
		return resource.NetworkPortsResponse{}, err
	}
	var respData struct {
		Ports []struct {
			ID             string   `json:"id"`
			SecurityGroups []string `json:"security_groups"`
		} `json:"ports"`
	}

	err = json.Unmarshal([]byte(body), &respData)
	if err != nil {
		ns.logger.WithError(err).Error("failed to unmarshal response body")
		return resource.NetworkPortsResponse{}, err
	}

	var result resource.NetworkPortsResponse
	for _, port := range respData.Ports {
		for _, sg := range port.SecurityGroups {
			if sg == securityGroupID {
				result.Ports = append(result.Ports, port.ID)
				break
			}
		}
	}

	return result, nil
}
