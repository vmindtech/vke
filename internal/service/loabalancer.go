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
	"time"

	"github.com/sirupsen/logrus"
	"github.com/vmindtech/vke/config"
	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/dto/resource"
)

type ILoadbalancerService interface {
	GetAmphoraesVrrpIp(authToken, loadBalancerID string) (resource.GetAmphoraesVrrpIpResponse, error)
	ListLoadBalancer(ctx context.Context, authToken, loadBalancerID string) (resource.ListLoadBalancerResponse, error)
	CreateLoadBalancer(ctx context.Context, authToken string, req request.CreateLoadBalancerRequest) (resource.CreateLoadBalancerResponse, error)
	CreateListener(ctx context.Context, authToken string, req request.CreateListenerRequest) (resource.CreateListenerResponse, error)
	CreatePool(ctx context.Context, authToken string, req request.CreatePoolRequest) (resource.CreatePoolResponse, error)
	CreateMember(ctx context.Context, authToken, poolID string, req request.AddMemberRequest) error
	ListListener(ctx context.Context, authToken, listenerID string) (resource.ListListenerResponse, error)
	CheckLoadBalancerStatus(ctx context.Context, authToken, loadBalancerID string) (resource.ListLoadBalancerResponse, error)
	CreateHealthHTTPMonitor(ctx context.Context, authToken string, req request.CreateHealthMonitorHTTPRequest) error
	CreateHealthTCPMonitor(ctx context.Context, authToken string, req request.CreateHealthMonitorTCPRequest) error
	CheckLoadBalancerOperationStatus(ctx context.Context, authToken, loadBalancerID string) (resource.ListLoadBalancerResponse, error)
	DeleteLoadbalancerPool(ctx context.Context, authToken, loadBalancerID string) error
	DeleteLoadbalancerListener(ctx context.Context, authToken, loadBalancerID string) error
	DeleteLoadbalancer(ctx context.Context, authToken, loadBalancerID string) error
}

type loadbalancerService struct {
	logger *logrus.Logger
}

func NewLoadbalancerService(logger *logrus.Logger) ILoadbalancerService {
	return &loadbalancerService{
		logger: logger,
	}
}

func (lbc *loadbalancerService) GetAmphoraesVrrpIp(authToken, loadBalancerID string) (resource.GetAmphoraesVrrpIpResponse, error) {
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/?loadbalancer_id=%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, amphoraePath, loadBalancerID), nil)
	if err != nil {
		lbc.logger.Errorf("failed to create request, error: %v", err)
		return resource.GetAmphoraesVrrpIpResponse{}, err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Auth-Token", authToken)
	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		lbc.logger.Errorf("failed to send request, error: %v", err)
		return resource.GetAmphoraesVrrpIpResponse{}, err
	}
	defer resp.Body.Close()
	var respDecoder resource.GetAmphoraesVrrpIpResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		lbc.logger.Errorf("failed to decode response, error: %v", err)
		return resource.GetAmphoraesVrrpIpResponse{}, err
	}
	if respDecoder.Amphorae == nil {
		lbc.logger.Errorf("Amphorae is nil")
		return resource.GetAmphoraesVrrpIpResponse{}, fmt.Errorf("Amphorae is nil")
	}
	return respDecoder, nil

}

func (lbc *loadbalancerService) ListLoadBalancer(ctx context.Context, authToken, loadBalancerID string) (resource.ListLoadBalancerResponse, error) {
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, loadBalancerPath, loadBalancerID), nil)
	if err != nil {
		lbc.logger.Errorf("failed to create request, error: %v", err)
		return resource.ListLoadBalancerResponse{}, err
	}
	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		lbc.logger.Errorf("failed to send request, error: %v", err)
		return resource.ListLoadBalancerResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		lbc.logger.Errorf("failed to list load balancer, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return resource.ListLoadBalancerResponse{}, fmt.Errorf("failed to list load balancer, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.ListLoadBalancerResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		lbc.logger.Errorf("failed to decode response, error: %v", err)
		return resource.ListLoadBalancerResponse{}, err
	}

	return respDecoder, nil
}

func (lbc *loadbalancerService) CreateLoadBalancer(ctx context.Context, authToken string, req request.CreateLoadBalancerRequest) (resource.CreateLoadBalancerResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		lbc.logger.Errorf("failed to marshal request, error: %v", err)
		return resource.CreateLoadBalancerResponse{}, err
	}
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, loadBalancerPath), bytes.NewBuffer(data))
	if err != nil {
		lbc.logger.Errorf("failed to create request, error: %v", err)
		return resource.CreateLoadBalancerResponse{}, err
	}
	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		lbc.logger.Errorf("failed to send request, error: %v", err)
		return resource.CreateLoadBalancerResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		lbc.logger.Errorf("failed to create load balancer, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return resource.CreateLoadBalancerResponse{}, fmt.Errorf("failed to create load balancer, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.CreateLoadBalancerResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		lbc.logger.Errorf("failed to decode response, error: %v", err)
		return resource.CreateLoadBalancerResponse{}, err
	}

	return respDecoder, nil
}

func (lbc *loadbalancerService) CreateListener(ctx context.Context, authToken string, req request.CreateListenerRequest) (resource.CreateListenerResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		lbc.logger.Errorf("failed to marshal request, error: %v", err)
		return resource.CreateListenerResponse{}, err
	}

	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, listenersPath), bytes.NewBuffer(data))
	if err != nil {
		lbc.logger.Errorf("failed to create request, error: %v", err)
		return resource.CreateListenerResponse{}, err
	}

	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		lbc.logger.Errorf("failed to send request, error: %v", err)
		return resource.CreateListenerResponse{}, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		lbc.logger.Errorf("failed to create listener, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return resource.CreateListenerResponse{}, fmt.Errorf("failed to create listener, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.CreateListenerResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		lbc.logger.Errorf("failed to decode response, error: %v", err)
		return resource.CreateListenerResponse{}, err
	}

	return respDecoder, nil
}

func (lbc *loadbalancerService) CreatePool(ctx context.Context, authToken string, req request.CreatePoolRequest) (resource.CreatePoolResponse, error) {
	data, err := json.Marshal(req)
	if err != nil {
		lbc.logger.Errorf("failed to marshal request, error: %v", err)
		return resource.CreatePoolResponse{}, err
	}

	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, ListenerPoolPath), bytes.NewBuffer(data))
	if err != nil {
		lbc.logger.Errorf("failed to create request, error: %v", err)
		return resource.CreatePoolResponse{}, err
	}

	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		lbc.logger.Errorf("failed to send request, error: %v", err)
		return resource.CreatePoolResponse{}, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		lbc.logger.Errorf("failed to create pool, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		b, err := httputil.DumpResponse(resp, true)
		if err != nil {
			log.Fatalln(err)
		}

		return resource.CreatePoolResponse{}, fmt.Errorf("failed to create pool, status code: %v, error msg: %v", resp.StatusCode, string(b))
	}

	var respDecoder resource.CreatePoolResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		lbc.logger.Errorf("failed to decode response, error: %v", err)
		return resource.CreatePoolResponse{}, err
	}

	return respDecoder, nil
}

func (lbc *loadbalancerService) CreateMember(ctx context.Context, authToken, poolID string, req request.AddMemberRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		lbc.logger.Errorf("failed to marshal request, error: %v", err)
		return err
	}

	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s/%s/members", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, createMemberPath, poolID), bytes.NewBuffer(data))
	if err != nil {
		lbc.logger.Errorf("failed to create request, error: %v", err)
		return err
	}

	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		lbc.logger.Errorf("failed to send request, error: %v", err)
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, err := httputil.DumpResponse(resp, true)
		if err != nil {
			log.Fatalln(err)
		}
		lbc.logger.Errorf("failed to create member, status code: %v, error msg: %v", resp.StatusCode, string(b))
		return fmt.Errorf("failed to create member, status code: %v, error msg: %v", resp.StatusCode, string(b))
	}

	var respDecoder resource.AddMemberResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		lbc.logger.Errorf("failed to decode response, error: %v", err)
		return err
	}

	return nil
}

func (lbc *loadbalancerService) ListListener(ctx context.Context, authToken, listenerID string) (resource.ListListenerResponse, error) {
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, listenersPath, listenerID), nil)
	if err != nil {
		lbc.logger.Errorf("failed to create request, error: %v", err)
		return resource.ListListenerResponse{}, err
	}
	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		lbc.logger.Errorf("failed to send request, error: %v", err)
		return resource.ListListenerResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		lbc.logger.Errorf("failed to list listener, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return resource.ListListenerResponse{}, fmt.Errorf("failed to list listener, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.ListListenerResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		lbc.logger.Errorf("failed to decode response, error: %v", err)
		return resource.ListListenerResponse{}, err
	}

	return respDecoder, nil
}

func (lbc *loadbalancerService) CheckLoadBalancerStatus(ctx context.Context, authToken, loadBalancerID string) (resource.ListLoadBalancerResponse, error) {
	waitIterator := 0
	waitSeconds := 10
	for {
		if waitIterator < 16 {
			time.Sleep(time.Duration(waitSeconds) * time.Second)
			fmt.Printf("Waiting for load balancer to be active, waited %v seconds\n", waitSeconds)
			waitIterator++
			waitSeconds = waitSeconds + 5
		} else {
			err := fmt.Errorf("failed to create load balancer, provisioning status is not ACTIVE")
			return resource.ListLoadBalancerResponse{}, err
		}
		listLBResp, err := lbc.ListLoadBalancer(ctx, authToken, loadBalancerID)
		if err != nil {
			lbc.logger.Errorf("failed to list load balancer, error: %v", err)
			return resource.ListLoadBalancerResponse{}, err
		}
		if listLBResp.LoadBalancer.ProvisioningStatus == LoadBalancerStatusError {
			err := fmt.Errorf("failed to create load balancer, provisioning status is ERROR")
			return resource.ListLoadBalancerResponse{}, err
		}
		if listLBResp.LoadBalancer.ProvisioningStatus == LoadBalancerStatusActive {
			break
		}
	}
	return resource.ListLoadBalancerResponse{}, nil
}

func (lbc *loadbalancerService) CreateHealthHTTPMonitor(ctx context.Context, authToken string, req request.CreateHealthMonitorHTTPRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		lbc.logger.Errorf("failed to marshal request, error: %v", err)
		return err
	}
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, healthMonitorPath), bytes.NewBuffer(data))
	if err != nil {
		lbc.logger.Errorf("failed to create request, error: %v", err)
		return err
	}
	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		lbc.logger.Errorf("failed to send request, error: %v", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, err := httputil.DumpResponse(resp, true)
		if err != nil {
			log.Fatalln(err)
		}

		lbc.logger.Errorf("failed to create health monitor, status code: %v, error msg: %v", resp.StatusCode, string(b))
		return fmt.Errorf("failed to create health monitor, status code: %v, error msg: %v", resp.StatusCode, string(b))
	}

	var respDecoder resource.CreateHealthMonitorResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		lbc.logger.Errorf("failed to decode response, error: %v", err)
		return err
	}

	return nil
}

func (lbc *loadbalancerService) CreateHealthTCPMonitor(ctx context.Context, authToken string, req request.CreateHealthMonitorTCPRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		lbc.logger.Errorf("failed to marshal request, error: %v", err)
		return err
	}
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, healthMonitorPath), bytes.NewBuffer(data))
	if err != nil {
		lbc.logger.Errorf("failed to create request, error: %v", err)
		return err
	}
	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		lbc.logger.Errorf("failed to send request, error: %v", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, err := httputil.DumpResponse(resp, true)
		if err != nil {
			log.Fatalln(err)
		}

		lbc.logger.Errorf("failed to create health monitor, status code: %v, error msg: %v", resp.StatusCode, string(b))
		return fmt.Errorf("failed to create health monitor, status code: %v, error msg: %v", resp.StatusCode, string(b))
	}

	var respDecoder resource.CreateHealthMonitorResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		lbc.logger.Errorf("failed to decode response, error: %v", err)
		return err
	}

	return nil
}

func (lbc *loadbalancerService) CheckLoadBalancerOperationStatus(ctx context.Context, authToken, loadBalancerID string) (resource.ListLoadBalancerResponse, error) {
	waitIterator := 0
	waitSeconds := 35
	for {
		if waitIterator < 16 {
			time.Sleep(time.Duration(waitSeconds) * time.Second)
			fmt.Printf("Waiting for load balancer operation to be ONLINE, waited %v seconds\n", waitSeconds)
			waitIterator++
			waitSeconds = waitSeconds + 5
		} else {
			return resource.ListLoadBalancerResponse{}, fmt.Errorf("failed to create load balancer, operation status is not ONLINE")
		}
		listLBResp, err := lbc.ListLoadBalancer(ctx, authToken, loadBalancerID)
		if err != nil {
			lbc.logger.Errorf("failed to list load balancer, error: %v", err)
			return resource.ListLoadBalancerResponse{}, err
		}
		if listLBResp.LoadBalancer.OperatingStatus == "ONLINE" {
			break
		}
	}
	return resource.ListLoadBalancerResponse{}, nil
}

func (lbc *loadbalancerService) DeleteLoadbalancerPool(ctx context.Context, authToken, loadBalancerID string) error {
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, loadBalancerPath, loadBalancerID), nil)
	if err != nil {
		lbc.logger.Errorf("failed to create request, error: %v", err)
		return err
	}

	r.Header.Add("X-Auth-Token", authToken)

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		lbc.logger.Errorf("failed to send request, error: %v", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		lbc.logger.Errorf("failed to list load balancer, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return fmt.Errorf("failed to list load balancer, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		lbc.logger.Errorf("failed to read response body, error: %v", err)
		return err
	}
	var respdata map[string]map[string]interface{}
	err = json.Unmarshal([]byte(body), &respdata)
	if err != nil {
		lbc.logger.Errorf("failed to unmarshal response body, error: %v", err)
		return err
	}

	poolsInterface := respdata["loadbalancer"]["pools"]

	pools := poolsInterface.([]interface{})
	waitIterator := 0
	waitSeconds := 10
	for _, pool := range pools {
		poolID := pool.(map[string]interface{})["id"].(string)
		r, err = http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, ListenerPoolPath, poolID), nil)
		if err != nil {
			lbc.logger.Errorf("failed to create request, error: %v", err)
			return err
		}

		r.Header.Add("X-Auth-Token", authToken)

		client = &http.Client{}
		resp, err = client.Do(r)
		if err != nil {
			lbc.logger.Errorf("failed to send request, error: %v", err)
			return err
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNoContent {
			lbc.logger.Errorf("failed to delete load balancer pool, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
			return fmt.Errorf("failed to delete load balancer pool, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		}

		for {
			if waitIterator < 8 {
				time.Sleep(time.Duration(waitSeconds) * time.Second)
				fmt.Printf("Waiting for load balancer pool to be deleted, waited %v seconds\n", waitSeconds)
				waitIterator++
				waitSeconds = waitSeconds + 5
				r, err = http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, ListenerPoolPath, poolID), nil)
				if err != nil {
					lbc.logger.Errorf("failed to create request, error: %v", err)
					return err
				}

				r.Header.Add("X-Auth-Token", authToken)

				client = &http.Client{}
				resp, err = client.Do(r)
				if err != nil {
					lbc.logger.Errorf("failed to send request, error: %v", err)
					return err
				}
				defer resp.Body.Close()

				if resp.StatusCode == http.StatusNotFound {
					break
				}
			} else {
				return fmt.Errorf("failed to delete load balancer pool, provisioning status is not DELETED")
			}
		}
	}

	return nil
}

func (lbc *loadbalancerService) DeleteLoadbalancerListener(ctx context.Context, authToken, loadBalancerID string) error {
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, loadBalancerPath, loadBalancerID), nil)
	if err != nil {
		lbc.logger.Errorf("failed to create request, error: %v", err)
		return err
	}

	r.Header.Add("X-Auth-Token", authToken)

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		lbc.logger.Errorf("failed to send request, error: %v", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		lbc.logger.Errorf("failed to list load balancer, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return fmt.Errorf("failed to list load balancer, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		lbc.logger.Errorf("failed to read response body, error: %v", err)
		return err
	}
	var respdata map[string]map[string]interface{}
	err = json.Unmarshal([]byte(body), &respdata)
	if err != nil {
		lbc.logger.Errorf("failed to unmarshal response body, error: %v", err)
		return err
	}

	listenersInterface := respdata["loadbalancer"]["listeners"]

	listeners := listenersInterface.([]interface{})

	waitIterator := 0
	waitSeconds := 10

	for _, listener := range listeners {
		listenerID := listener.(map[string]interface{})["id"].(string)
		r, err = http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, listenersPath, listenerID), nil)
		if err != nil {
			lbc.logger.Errorf("failed to create request, error: %v", err)
			return err
		}

		r.Header.Add("X-Auth-Token", authToken)

		client = &http.Client{}
		resp, err = client.Do(r)
		if err != nil {
			lbc.logger.Errorf("failed to send request, error: %v", err)
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			lbc.logger.Errorf("failed to delete load balancer listener, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
			return fmt.Errorf("failed to delete load balancer listener, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		}

		for {
			if waitIterator < 8 {
				time.Sleep(time.Duration(waitSeconds) * time.Second)
				fmt.Printf("Waiting for load balancer listener to be deleted, waited %v seconds\n", waitSeconds)
				waitIterator++
				waitSeconds = waitSeconds + 5
				r, err = http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, listenersPath, listenerID), nil)
				if err != nil {
					lbc.logger.Errorf("failed to create request, error: %v", err)
					return err
				}

				r.Header.Add("X-Auth-Token", authToken)

				client = &http.Client{}
				resp, err = client.Do(r)
				if err != nil {
					lbc.logger.Errorf("failed to send request, error: %v", err)
					return err
				}
				defer resp.Body.Close()

				if resp.StatusCode == http.StatusNotFound {
					break
				}
			} else {
				return fmt.Errorf("failed to delete load balancer listener, provisioning status is not DELETED")
			}
		}
	}
	return nil
}

func (lbc *loadbalancerService) DeleteLoadbalancer(ctx context.Context, authToken, loadBalancerID string) error {
	r, err := http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, loadBalancerPath, loadBalancerID), nil)
	if err != nil {
		lbc.logger.Errorf("failed to create request, error: %v", err)
		return err
	}

	r.Header.Add("X-Auth-Token", authToken)

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		lbc.logger.Errorf("failed to send request, error: %v", err)
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		lbc.logger.Errorf("failed to delete load balancer, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return fmt.Errorf("failed to delete load balancer, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	return nil
}
