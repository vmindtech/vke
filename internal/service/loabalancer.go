package service

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/vmindtech/vke/config"
	"github.com/vmindtech/vke/internal/dto/resource"
)

func GetAmphoraesVrrpIp(authToken, loadBalancerID string) (resource.GetAmphoraesVrrpIpResponse, error) {
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/?loadbalancer_id=%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, amphoraePath, loadBalancerID), nil)
	if err != nil {
		return resource.GetAmphoraesVrrpIpResponse{}, err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Auth-Token", authToken)
	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		return resource.GetAmphoraesVrrpIpResponse{}, err
	}
	defer resp.Body.Close()
	var respDecoder resource.GetAmphoraesVrrpIpResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		return resource.GetAmphoraesVrrpIpResponse{}, err
	}
	return respDecoder, nil

}
