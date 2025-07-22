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
	"time"

	"github.com/sirupsen/logrus"
	"github.com/vmindtech/vke/config"
	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/pkg/constants"
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
	DeleteLoadbalancer(ctx context.Context, authToken, loadBalancerID string) error
	GetLoadBalancerPools(ctx context.Context, authToken, loadBalancerID string) (resource.GetLoadBalancerPoolsResponse, error)
	CheckLoadBalancerDeletingPools(ctx context.Context, authToken, poolID string) error
	DeleteLoadbalancerPools(ctx context.Context, authToken, poolID string) error
	GetLoadBalancerListeners(ctx context.Context, authToken, loadBalancerID string) (resource.GetLoadBalancerListenersResponse, error)
	DeleteLoadbalancerListeners(ctx context.Context, authToken, listenerID string) error
	CheckLoadBalancerDeletingListeners(ctx context.Context, authToken, listenerID string) error
}

type loadbalancerService struct {
	logger *logrus.Logger
	client http.Client
}

func NewLoadbalancerService(logger *logrus.Logger) ILoadbalancerService {
	return &loadbalancerService{
		logger: logger,
		client: CreateHTTPClient(),
	}
}

func (lbc *loadbalancerService) GetAmphoraesVrrpIp(authToken, loadBalancerID string) (resource.GetAmphoraesVrrpIpResponse, error) {
	token := strings.Clone(authToken)
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/?loadbalancer_id=%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, constants.AmphoraePath, loadBalancerID), nil)
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"loadBalancerID": loadBalancerID,
		}).WithError(err).Error("failed to create request")
		return resource.GetAmphoraesVrrpIpResponse{}, err
	}
	r.Header = make(http.Header)
	r.Header.Add("Content-Type", "application/json")
	r.Header.Add("X-Auth-Token", token)
	resp, err := lbc.client.Do(r)
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"loadBalancerID": loadBalancerID,
		}).WithError(err).Error("failed to send request")
		return resource.GetAmphoraesVrrpIpResponse{}, err
	}
	defer resp.Body.Close()
	var respDecoder resource.GetAmphoraesVrrpIpResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"loadBalancerID": loadBalancerID,
		}).WithError(err).Error("failed to decode response")
		return resource.GetAmphoraesVrrpIpResponse{}, err
	}
	if respDecoder.Amphorae == nil {
		lbc.logger.WithFields(logrus.Fields{
			"loadBalancerID": loadBalancerID,
		}).Error("Amphorae is nil")
		return resource.GetAmphoraesVrrpIpResponse{}, fmt.Errorf("Amphorae is nil")
	}
	return respDecoder, nil

}

func (lbc *loadbalancerService) ListLoadBalancer(ctx context.Context, authToken, loadBalancerID string) (resource.ListLoadBalancerResponse, error) {
	token := strings.Clone(authToken)

	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, constants.LoadBalancerPath, loadBalancerID), nil)
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"loadBalancerID": loadBalancerID,
		}).WithError(err).Error("failed to create request")
		return resource.ListLoadBalancerResponse{}, err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)
	r.Header.Add("Content-Type", "application/json")
	resp, err := lbc.client.Do(r)
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"loadBalancerID": loadBalancerID,
		}).WithError(err).Error("failed to send request")
		return resource.ListLoadBalancerResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		lbc.logger.WithFields(logrus.Fields{
			"loadBalancerID": loadBalancerID,
		}).WithError(err).Error("failed to list load balancer")
		return resource.ListLoadBalancerResponse{}, fmt.Errorf("failed to list load balancer, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.ListLoadBalancerResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"loadBalancerID": loadBalancerID,
		}).WithError(err).Error("failed to decode response")
		return resource.ListLoadBalancerResponse{}, err
	}

	return respDecoder, nil
}

func (lbc *loadbalancerService) CreateLoadBalancer(ctx context.Context, authToken string, req request.CreateLoadBalancerRequest) (resource.CreateLoadBalancerResponse, error) {
	token := strings.Clone(authToken)
	data, err := json.Marshal(req)
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"loadBalancerName": req.LoadBalancer.Name,
		}).WithError(err).Error("failed to marshal request")
		return resource.CreateLoadBalancerResponse{}, err
	}
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, constants.LoadBalancerPath), bytes.NewBuffer(data))
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"loadBalancerName": req.LoadBalancer.Name,
		}).WithError(err).Error("failed to create request")
		return resource.CreateLoadBalancerResponse{}, err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)
	r.Header.Add("Content-Type", "application/json")

	resp, err := lbc.client.Do(r)
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"loadBalancerName": req.LoadBalancer.Name,
		}).WithError(err).Error("failed to send request")
		return resource.CreateLoadBalancerResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		lbc.logger.WithFields(logrus.Fields{
			"loadBalancerName": req.LoadBalancer.Name,
			"statusCode":       resp.StatusCode,
			"status":           resp.Status,
		}).Error("failed to create load balancer")
		return resource.CreateLoadBalancerResponse{}, fmt.Errorf("failed to create load balancer, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.CreateLoadBalancerResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"loadBalancerName": req.LoadBalancer.Name,
		}).WithError(err).Error("failed to decode response")
		return resource.CreateLoadBalancerResponse{}, err
	}

	return respDecoder, nil
}

func (lbc *loadbalancerService) CreateListener(ctx context.Context, authToken string, req request.CreateListenerRequest) (resource.CreateListenerResponse, error) {
	token := strings.Clone(authToken)
	data, err := json.Marshal(req)
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"loadBalancerID": req.Listener.LoadbalancerID,
		}).WithError(err).Error("failed to marshal request")
		return resource.CreateListenerResponse{}, err
	}

	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, constants.ListenersPath), bytes.NewBuffer(data))
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"loadBalancerID": req.Listener.LoadbalancerID,
		}).WithError(err).Error("failed to create request")
		return resource.CreateListenerResponse{}, err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)
	r.Header.Add("Content-Type", "application/json")

	resp, err := lbc.client.Do(r)
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"loadBalancerID": req.Listener.LoadbalancerID,
		}).WithError(err).Error("failed to send request")
		return resource.CreateListenerResponse{}, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		lbc.logger.WithFields(logrus.Fields{
			"loadBalancerID": req.Listener.LoadbalancerID,
			"statusCode":     resp.StatusCode,
			"status":         resp.Status,
		}).Error("failed to create listener")
		return resource.CreateListenerResponse{}, fmt.Errorf("failed to create listener, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.CreateListenerResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"loadBalancerID": req.Listener.LoadbalancerID,
		}).WithError(err).Error("failed to decode response")
		return resource.CreateListenerResponse{}, err
	}

	return respDecoder, nil
}

func (lbc *loadbalancerService) CreatePool(ctx context.Context, authToken string, req request.CreatePoolRequest) (resource.CreatePoolResponse, error) {
	token := strings.Clone(authToken)
	data, err := json.Marshal(req)
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"listenerID": req.Pool.ListenerID,
		}).WithError(err).Error("failed to marshal request")
		return resource.CreatePoolResponse{}, err
	}

	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, constants.ListenerPoolPath), bytes.NewBuffer(data))
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"listenerID": req.Pool.ListenerID,
		}).WithError(err).Error("failed to create request")
		return resource.CreatePoolResponse{}, err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)
	r.Header.Add("Content-Type", "application/json")

	resp, err := lbc.client.Do(r)
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"listenerID": req.Pool.ListenerID,
		}).WithError(err).Error("failed to send request")
		return resource.CreatePoolResponse{}, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		lbc.logger.WithFields(logrus.Fields{
			"listenerID": req.Pool.ListenerID,
			"statusCode": resp.StatusCode,
			"status":     resp.Status,
		}).Error("failed to create pool")
		b, err := httputil.DumpResponse(resp, true)
		if err != nil {
			log.Fatalln(err)
		}

		return resource.CreatePoolResponse{}, fmt.Errorf("failed to create pool, status code: %v, error msg: %v", resp.StatusCode, string(b))
	}

	var respDecoder resource.CreatePoolResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"listenerID": req.Pool.ListenerID,
		}).WithError(err).Error("failed to decode response")
		return resource.CreatePoolResponse{}, err
	}

	return respDecoder, nil
}

func (lbc *loadbalancerService) CreateMember(ctx context.Context, authToken, poolID string, req request.AddMemberRequest) error {
	token := strings.Clone(authToken)
	data, err := json.Marshal(req)
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"poolID": poolID,
		}).WithError(err).Error("failed to marshal request")
		return err
	}

	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s/%s/members", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, constants.CreateMemberPath, poolID), bytes.NewBuffer(data))
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"poolID": poolID,
		}).WithError(err).Error("failed to create request")
		return err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)
	r.Header.Add("Content-Type", "application/json")

	resp, err := lbc.client.Do(r)
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"poolID": poolID,
		}).WithError(err).Error("failed to send request")
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, err := httputil.DumpResponse(resp, true)
		if err != nil {
			log.Fatalln(err)
		}
		// lbc.logger.Errorf("failed to create member, status code: %v, error msg: %v", resp.StatusCode, string(b))
		lbc.logger.WithFields(logrus.Fields{
			"poolID":     poolID,
			"statusCode": resp.StatusCode,
			"status":     resp.Status,
			"error":      string(b),
		}).Error("failed to create member")
		return fmt.Errorf("failed to create member, status code: %v, error msg: %v", resp.StatusCode, string(b))
	}

	var respDecoder resource.AddMemberResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"poolID": poolID,
		}).WithError(err).Error("failed to decode response")
		return err
	}

	return nil
}

func (lbc *loadbalancerService) ListListener(ctx context.Context, authToken, listenerID string) (resource.ListListenerResponse, error) {
	token := strings.Clone(authToken)
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, constants.ListenersPath, listenerID), nil)
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"listenerID": listenerID,
		}).WithError(err).Error("failed to create request")
		return resource.ListListenerResponse{}, err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)
	r.Header.Add("Content-Type", "application/json")

	resp, err := lbc.client.Do(r)
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"listenerID": listenerID,
		}).WithError(err).Error("failed to send request")
		return resource.ListListenerResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		lbc.logger.WithFields(logrus.Fields{
			"listenerID": listenerID,
			"statusCode": resp.StatusCode,
			"status":     resp.Status,
		}).Error("failed to list listener")
		return resource.ListListenerResponse{}, fmt.Errorf("failed to list listener, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.ListListenerResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"listenerID": listenerID,
		}).WithError(err).Error("failed to decode response")
		return resource.ListListenerResponse{}, err
	}

	return respDecoder, nil
}

func (lbc *loadbalancerService) CheckLoadBalancerStatus(ctx context.Context, authToken, loadBalancerID string) (resource.ListLoadBalancerResponse, error) {
	token := strings.Clone(authToken)
	waitIterator := 0
	waitSeconds := 1
	for {
		if waitIterator < 16 {
			time.Sleep(time.Duration(waitSeconds) * time.Second)
			lbc.logger.WithFields(logrus.Fields{
				"loadBalancerID": loadBalancerID,
				"waitedSeconds":  waitSeconds,
			}).Info("Waiting for load balancer to be ACTIVE")
			waitIterator++
			waitSeconds = waitSeconds + 5
		} else {
			err := fmt.Errorf("failed to create load balancer, provisioning status is not ACTIVE")
			return resource.ListLoadBalancerResponse{}, err
		}
		listLBResp, err := lbc.ListLoadBalancer(ctx, token, loadBalancerID)
		if err != nil {
			lbc.logger.WithFields(logrus.Fields{
				"loadBalancerID": loadBalancerID,
			}).WithError(err).Error("failed to list load balancer")
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
	token := strings.Clone(authToken)
	data, err := json.Marshal(req)
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"poolID": req.HealthMonitor.PoolID,
		}).WithError(err).Error("failed to marshal request")
		return err
	}
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, constants.HealthMonitorPath), bytes.NewBuffer(data))
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"poolID": req.HealthMonitor.PoolID,
		}).WithError(err).Error("failed to create request")
		return err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)
	r.Header.Add("Content-Type", "application/json")

	resp, err := lbc.client.Do(r)
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"poolID": req.HealthMonitor.PoolID,
		}).WithError(err).Error("failed to send request")
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, err := httputil.DumpResponse(resp, true)
		if err != nil {
			log.Fatalln(err)
		}

		lbc.logger.WithFields(logrus.Fields{
			"poolID":     req.HealthMonitor.PoolID,
			"statusCode": resp.StatusCode,
			"status":     resp.Status,
			"error":      string(b),
		}).Error("failed to create health monitor")
		return fmt.Errorf("failed to create health monitor, status code: %v, error msg: %v", resp.StatusCode, string(b))
	}

	var respDecoder resource.CreateHealthMonitorResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"poolID": req.HealthMonitor.PoolID,
		}).WithError(err).Error("failed to decode response")
		return err
	}

	return nil
}

func (lbc *loadbalancerService) CreateHealthTCPMonitor(ctx context.Context, authToken string, req request.CreateHealthMonitorTCPRequest) error {
	token := strings.Clone(authToken)
	data, err := json.Marshal(req)
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"poolID": req.HealthMonitor.PoolID,
		}).WithError(err).Error("failed to marshal request")
		return err
	}
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, constants.HealthMonitorPath), bytes.NewBuffer(data))
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"poolID": req.HealthMonitor.PoolID,
		}).WithError(err).Error("failed to create request")
		return err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)
	r.Header.Add("Content-Type", "application/json")

	resp, err := lbc.client.Do(r)
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"poolID": req.HealthMonitor.PoolID,
		}).WithError(err).Error("failed to send request")
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, err := httputil.DumpResponse(resp, true)
		if err != nil {
			log.Fatalln(err)
		}

		lbc.logger.WithFields(logrus.Fields{
			"poolID":     req.HealthMonitor.PoolID,
			"statusCode": resp.StatusCode,
			"status":     resp.Status,
			"error":      string(b),
		}).Error("failed to create health monitor")
		return fmt.Errorf("failed to create health monitor, status code: %v, error msg: %v", resp.StatusCode, string(b))
	}

	var respDecoder resource.CreateHealthMonitorResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"poolID": req.HealthMonitor.PoolID,
		}).WithError(err).Error("failed to decode response")
		return err
	}

	return nil
}

func (lbc *loadbalancerService) CheckLoadBalancerOperationStatus(ctx context.Context, authToken, loadBalancerID string) (resource.ListLoadBalancerResponse, error) {
	token := strings.Clone(authToken)
	waitIterator := 0
	waitSeconds := 35
	for {
		if waitIterator < 16 {
			time.Sleep(time.Duration(waitSeconds) * time.Second)
			lbc.logger.WithFields(logrus.Fields{
				"loadBalancerID": loadBalancerID,
				"waitedSeconds":  waitSeconds,
			}).Info("Waiting for load balancer operation to be ONLINE")
			waitIterator++
			waitSeconds = waitSeconds + 5
		} else {
			return resource.ListLoadBalancerResponse{}, fmt.Errorf("failed to create load balancer, operation status is not ONLINE")
		}
		listLBResp, err := lbc.ListLoadBalancer(ctx, token, loadBalancerID)
		if err != nil {
			lbc.logger.WithFields(logrus.Fields{
				"loadBalancerID": loadBalancerID,
			}).WithError(err).Error("failed to list load balancer")
			return resource.ListLoadBalancerResponse{}, err
		}
		if listLBResp.LoadBalancer.OperatingStatus == "ONLINE" {
			break
		}
	}
	return resource.ListLoadBalancerResponse{}, nil
}

func (lbc *loadbalancerService) GetLoadBalancerPools(ctx context.Context, authToken, loadBalancerID string) (resource.GetLoadBalancerPoolsResponse, error) {
	token := strings.Clone(authToken)
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, constants.LoadBalancerPath, loadBalancerID), nil)
	if err != nil {
		lbc.logger.WithError(err).Error("failed to create request")
		return resource.GetLoadBalancerPoolsResponse{}, err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)

	resp, err := lbc.client.Do(r)
	if err != nil {
		lbc.logger.WithError(err).Error("failed to send request")
		return resource.GetLoadBalancerPoolsResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		lbc.logger.WithFields(logrus.Fields{
			"statusCode": resp.StatusCode,
			"status":     resp.Status,
		}).Error("failed to list load balancer")
		return resource.GetLoadBalancerPoolsResponse{}, fmt.Errorf("failed to list load balancer, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		lbc.logger.WithError(err).Error("failed to read response body")
		return resource.GetLoadBalancerPoolsResponse{}, err
	}
	var respdata map[string]map[string]interface{}
	err = json.Unmarshal([]byte(body), &respdata)
	if err != nil {
		lbc.logger.WithError(err).Error("failed to unmarshal response body")
		return resource.GetLoadBalancerPoolsResponse{}, err
	}

	poolsInterface := respdata["loadbalancer"]["pools"]

	pools := poolsInterface.([]interface{})
	var respPools resource.GetLoadBalancerPoolsResponse

	for _, member := range pools {
		if poolMap, ok := member.(map[string]interface{}); ok {
			if id, exists := poolMap["id"].(string); exists {
				respPools.Pools = append(respPools.Pools, id)
			} else {
				lbc.logger.WithFields(logrus.Fields{
					"loadBalancerID": loadBalancerID,
				}).WithError(err).Error("failed to get pool id")
				return resource.GetLoadBalancerPoolsResponse{}, fmt.Errorf("failed to get pool id")
			}
		} else {
			lbc.logger.WithFields(logrus.Fields{
				"loadBalancerID": loadBalancerID,
			}).WithError(err).Error("failed to get pool id")
			return resource.GetLoadBalancerPoolsResponse{}, fmt.Errorf("failed to get pool id")
		}
	}

	return respPools, nil
}

func (lbc *loadbalancerService) DeleteLoadbalancerPools(ctx context.Context, authToken, poolID string) error {
	token := strings.Clone(authToken)
	r, err := http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, constants.ListenerPoolPath, poolID), nil)
	if err != nil {
		lbc.logger.WithError(err).Error("failed to create request")
		return err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)

	resp, err := lbc.client.Do(r)
	if err != nil {
		lbc.logger.WithError(err).Error("failed to send request")
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		lbc.logger.WithFields(logrus.Fields{
			"statusCode": resp.StatusCode,
			"status":     resp.Status,
		}).Error("failed to delete load balancer pool")
		return fmt.Errorf("failed to delete load balancer pool, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}
	return nil
}

func (lbc *loadbalancerService) CheckLoadBalancerDeletingPools(ctx context.Context, authToken, poolID string) error {
	token := strings.Clone(authToken)
	waitIterator := 0
	waitSeconds := 10
	for {
		if waitIterator < 8 {
			time.Sleep(time.Duration(waitSeconds) * time.Second)
			lbc.logger.WithFields(logrus.Fields{
				"poolID":        poolID,
				"waitedSeconds": waitSeconds,
			}).Info("Waiting for load balancer pool to be deleted")
			waitIterator++
			waitSeconds = waitSeconds + 5
			r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, constants.ListenerPoolPath, poolID), nil)
			if err != nil {
				lbc.logger.WithFields(logrus.Fields{
					"poolID": poolID,
				}).WithError(err).Error("failed to create request")
				return err
			}
			r.Header = make(http.Header)
			r.Header.Add("X-Auth-Token", token)

			resp, err := lbc.client.Do(r)
			if err != nil {
				lbc.logger.WithFields(logrus.Fields{
					"poolID": poolID,
				}).WithError(err).Error("failed to send request")
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

	return nil

}
func (lbc *loadbalancerService) GetLoadBalancerListeners(ctx context.Context, authToken, loadBalancerID string) (resource.GetLoadBalancerListenersResponse, error) {
	token := strings.Clone(authToken)
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, constants.LoadBalancerPath, loadBalancerID), nil)
	if err != nil {
		lbc.logger.WithError(err).Error("failed to create request")
		return resource.GetLoadBalancerListenersResponse{}, err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)

	resp, err := lbc.client.Do(r)
	if err != nil {
		lbc.logger.WithError(err).Error("failed to send request")
		return resource.GetLoadBalancerListenersResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		lbc.logger.WithFields(logrus.Fields{
			"statusCode": resp.StatusCode,
			"status":     resp.Status,
		}).Error("failed to list load balancer")
		return resource.GetLoadBalancerListenersResponse{}, fmt.Errorf("failed to list load balancer, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		lbc.logger.WithError(err).Error("failed to read response body")
		return resource.GetLoadBalancerListenersResponse{}, err
	}
	var respdata map[string]map[string]interface{}
	err = json.Unmarshal([]byte(body), &respdata)
	if err != nil {
		lbc.logger.WithError(err).Error("failed to unmarshal response body")
		return resource.GetLoadBalancerListenersResponse{}, err
	}

	listenersInterface := respdata["loadbalancer"]["listeners"]

	listeners := listenersInterface.([]interface{})

	var respListeners resource.GetLoadBalancerListenersResponse

	for _, listener := range listeners {
		if listenerMap, ok := listener.(map[string]interface{}); ok {
			if id, exists := listenerMap["id"].(string); exists {
				respListeners.Listeners = append(respListeners.Listeners, id)
			} else {
				lbc.logger.WithError(err).Error("failed to get listener id")
				return resource.GetLoadBalancerListenersResponse{}, fmt.Errorf("failed to get listener id")
			}
		} else {
			lbc.logger.WithError(err).Error("failed to get listener id")
			return resource.GetLoadBalancerListenersResponse{}, fmt.Errorf("failed to get listener id")
		}
	}

	return respListeners, nil
}

func (lbc *loadbalancerService) DeleteLoadbalancerListeners(ctx context.Context, authToken, listenerID string) error {
	token := strings.Clone(authToken)
	r, err := http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, constants.ListenersPath, listenerID), nil)
	if err != nil {
		lbc.logger.WithError(err).Error("failed to create request")
		return err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)

	resp, err := lbc.client.Do(r)
	if err != nil {
		lbc.logger.WithError(err).Error("failed to send request")
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		lbc.logger.WithFields(logrus.Fields{
			"statusCode": resp.StatusCode,
			"status":     resp.Status,
		}).Error("failed to delete load balancer listener")
		return fmt.Errorf("failed to delete load balancer listener, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}
	return nil
}

func (lbc *loadbalancerService) CheckLoadBalancerDeletingListeners(ctx context.Context, authToken, listenerID string) error {
	token := strings.Clone(authToken)
	waitIterator := 0
	waitSeconds := 10
	for {
		if waitIterator < 8 {
			time.Sleep(time.Duration(waitSeconds) * time.Second)
			lbc.logger.WithFields(logrus.Fields{
				"listenerID":    listenerID,
				"waitedSeconds": waitSeconds,
			}).Info("Waiting for load balancer listener to be deleted")
			waitIterator++
			waitSeconds = waitSeconds + 5
			r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, constants.ListenersPath, listenerID), nil)
			if err != nil {
				lbc.logger.WithError(err).Error("failed to create request")
				return err
			}
			r.Header = make(http.Header)
			r.Header.Add("X-Auth-Token", token)

			resp, err := lbc.client.Do(r)
			if err != nil {
				lbc.logger.WithError(err).Error("failed to send request")
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

	return nil

}

func (lbc *loadbalancerService) DeleteLoadbalancer(ctx context.Context, authToken, loadBalancerID string) error {
	token := strings.Clone(authToken)
	r, err := http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, constants.LoadBalancerPath, loadBalancerID), nil)
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"loadBalancerID": loadBalancerID,
		}).WithError(err).Error("failed to create request")
		return err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)

	resp, err := lbc.client.Do(r)
	if err != nil {
		lbc.logger.WithFields(logrus.Fields{
			"loadBalancerID": loadBalancerID,
		}).WithError(err).Error("failed to send request")
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		lbc.logger.WithFields(logrus.Fields{
			"loadBalancerID": loadBalancerID,
			"statusCode":     resp.StatusCode,
			"status":         resp.Status,
		}).Error("failed to delete load balancer")
		return fmt.Errorf("failed to delete load balancer, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	return nil
}
