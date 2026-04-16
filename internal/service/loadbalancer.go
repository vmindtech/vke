package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/vmindtech/vke/config"
	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/pkg/constants"
	"github.com/vmindtech/vke/pkg/ctxutil"
)

type ILoadbalancerService interface {
	GetAmphoraesVrrpIp(authToken, loadBalancerID string) (resource.GetAmphoraesVrrpIpResponse, error)
	ListLoadBalancer(ctx context.Context, authToken, loadBalancerID string) (resource.ListLoadBalancerResponse, error)
	// FindLoadBalancerIDByName lists load balancers by exact name (Octavia filter). Returns empty string if none.
	FindLoadBalancerIDByName(ctx context.Context, authToken, name string) (string, error)
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
	// WaitForLoadBalancerDeleted polls until GET load balancer returns 404 (async delete completed).
	WaitForLoadBalancerDeleted(ctx context.Context, authToken, loadBalancerID string) error
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

func (lbc *loadbalancerService) withLog(ctx context.Context) *logrus.Entry {
	if id := ctxutil.ClusterUUID(ctx); id != "" {
		return lbc.logger.WithField("clusterUUID", id)
	}
	return logrus.NewEntry(lbc.logger)
}

func (lbc *loadbalancerService) GetAmphoraesVrrpIp(authToken, loadBalancerID string) (resource.GetAmphoraesVrrpIpResponse, error) {
	bg := context.Background()
	token := strings.Clone(authToken)
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/?loadbalancer_id=%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, constants.AmphoraePath, loadBalancerID), nil)
	if err != nil {
		lbc.withLog(bg).WithFields(logrus.Fields{
			"loadBalancerID": loadBalancerID,
		}).WithError(err).Error("failed to create request")
		return resource.GetAmphoraesVrrpIpResponse{}, err
	}
	r.Header = make(http.Header)
	r.Header.Add("Content-Type", "application/json")
	r.Header.Add("X-Auth-Token", token)
	resp, err := lbc.client.Do(r)
	if err != nil {
		lbc.withLog(bg).WithFields(logrus.Fields{
			"loadBalancerID": loadBalancerID,
		}).WithError(err).Error("failed to send request")
		return resource.GetAmphoraesVrrpIpResponse{}, err
	}
	defer resp.Body.Close()
	var respDecoder resource.GetAmphoraesVrrpIpResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		lbc.withLog(bg).WithFields(logrus.Fields{
			"loadBalancerID": loadBalancerID,
		}).WithError(err).Error("failed to decode response")
		return resource.GetAmphoraesVrrpIpResponse{}, err
	}
	if respDecoder.Amphorae == nil {
		lbc.withLog(bg).WithFields(logrus.Fields{
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
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"loadBalancerID": loadBalancerID,
		}).WithError(err).Error("failed to create request")
		return resource.ListLoadBalancerResponse{}, err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)
	r.Header.Add("Content-Type", "application/json")
	resp, err := lbc.client.Do(r)
	if err != nil {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"loadBalancerID": loadBalancerID,
		}).WithError(err).Error("failed to send request")
		return resource.ListLoadBalancerResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"loadBalancerID": loadBalancerID,
		}).WithError(err).Error("failed to list load balancer")
		return resource.ListLoadBalancerResponse{}, fmt.Errorf("failed to list load balancer, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.ListLoadBalancerResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"loadBalancerID": loadBalancerID,
		}).WithError(err).Error("failed to decode response")
		return resource.ListLoadBalancerResponse{}, err
	}

	return respDecoder, nil
}

func (lbc *loadbalancerService) FindLoadBalancerIDByName(ctx context.Context, authToken, name string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", nil
	}
	token := strings.Clone(authToken)
	u := fmt.Sprintf("%s/%s?name=%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, constants.LoadBalancerPath, url.QueryEscape(name))
	r, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return "", err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)
	r.Header.Add("Content-Type", "application/json")
	resp, err := lbc.client.Do(r)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("list load balancers by name: status %d: %s", resp.StatusCode, string(b))
	}
	var out resource.ListLoadBalancersCollectionResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Loadbalancers) == 0 {
		return "", nil
	}
	return out.Loadbalancers[0].ID, nil
}

func (lbc *loadbalancerService) CreateLoadBalancer(ctx context.Context, authToken string, req request.CreateLoadBalancerRequest) (resource.CreateLoadBalancerResponse, error) {
	token := strings.Clone(authToken)
	data, err := json.Marshal(req)
	if err != nil {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"loadBalancerName": req.LoadBalancer.Name,
		}).WithError(err).Error("failed to marshal request")
		return resource.CreateLoadBalancerResponse{}, err
	}
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, constants.LoadBalancerPath), bytes.NewBuffer(data))
	if err != nil {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"loadBalancerName": req.LoadBalancer.Name,
		}).WithError(err).Error("failed to create request")
		return resource.CreateLoadBalancerResponse{}, err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)
	r.Header.Add("Content-Type", "application/json")

	resp, err := lbc.client.Do(r)
	if err != nil {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"loadBalancerName": req.LoadBalancer.Name,
		}).WithError(err).Error("failed to send request")
		return resource.CreateLoadBalancerResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"loadBalancerName": req.LoadBalancer.Name,
			"statusCode":       resp.StatusCode,
			"status":           resp.Status,
		}).Error("failed to create load balancer")
		return resource.CreateLoadBalancerResponse{}, fmt.Errorf("failed to create load balancer, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.CreateLoadBalancerResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		lbc.withLog(ctx).WithFields(logrus.Fields{
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
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"loadBalancerID": req.Listener.LoadbalancerID,
		}).WithError(err).Error("failed to marshal request")
		return resource.CreateListenerResponse{}, err
	}

	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, constants.ListenersPath), bytes.NewBuffer(data))
	if err != nil {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"loadBalancerID": req.Listener.LoadbalancerID,
		}).WithError(err).Error("failed to create request")
		return resource.CreateListenerResponse{}, err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)
	r.Header.Add("Content-Type", "application/json")

	resp, err := lbc.client.Do(r)
	if err != nil {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"loadBalancerID": req.Listener.LoadbalancerID,
		}).WithError(err).Error("failed to send request")
		return resource.CreateListenerResponse{}, err
	}

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated {
		var respDecoder resource.CreateListenerResponse
		if err := json.NewDecoder(resp.Body).Decode(&respDecoder); err != nil {
			lbc.withLog(ctx).WithFields(logrus.Fields{
				"loadBalancerID": req.Listener.LoadbalancerID,
			}).WithError(err).Error("failed to decode response")
			return resource.CreateListenerResponse{}, err
		}
		return respDecoder, nil
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyStr := string(bodyBytes)
	if isOctaviaDuplicateResourceConflict(resp.StatusCode, bodyStr) {
		lid, ferr := lbc.findListenerIDByLoadBalancerAndPort(ctx, token, req.Listener.LoadbalancerID, req.Listener.Protocol, req.Listener.ProtocolPort)
		if ferr != nil {
			return resource.CreateListenerResponse{}, fmt.Errorf("create listener conflict but could not resolve existing listener: %w (body: %s)", ferr, bodyStr)
		}
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"loadBalancerID": req.Listener.LoadbalancerID,
			"listenerID":     lid,
		}).Info("listener already exists for LB/port, reusing id")
		return resource.CreateListenerResponse{Listener: resource.CreateListener{ID: lid}}, nil
	}

	lbc.withLog(ctx).WithFields(logrus.Fields{
		"loadBalancerID": req.Listener.LoadbalancerID,
		"statusCode":     resp.StatusCode,
		"error":          bodyStr,
	}).Error("failed to create listener")
	return resource.CreateListenerResponse{}, fmt.Errorf("failed to create listener, status code: %v, error msg: %v", resp.StatusCode, bodyStr)
}

func (lbc *loadbalancerService) CreatePool(ctx context.Context, authToken string, req request.CreatePoolRequest) (resource.CreatePoolResponse, error) {
	token := strings.Clone(authToken)
	data, err := json.Marshal(req)
	if err != nil {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"listenerID": req.Pool.ListenerID,
		}).WithError(err).Error("failed to marshal request")
		return resource.CreatePoolResponse{}, err
	}

	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, constants.ListenerPoolPath), bytes.NewBuffer(data))
	if err != nil {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"listenerID": req.Pool.ListenerID,
		}).WithError(err).Error("failed to create request")
		return resource.CreatePoolResponse{}, err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)
	r.Header.Add("Content-Type", "application/json")

	resp, err := lbc.client.Do(r)
	if err != nil {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"listenerID": req.Pool.ListenerID,
		}).WithError(err).Error("failed to send request")
		return resource.CreatePoolResponse{}, err
	}

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated {
		var respDecoder resource.CreatePoolResponse
		if err := json.NewDecoder(resp.Body).Decode(&respDecoder); err != nil {
			lbc.withLog(ctx).WithFields(logrus.Fields{
				"listenerID": req.Pool.ListenerID,
			}).WithError(err).Error("failed to decode response")
			return resource.CreatePoolResponse{}, err
		}
		return respDecoder, nil
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyStr := string(bodyBytes)
	if isOctaviaDuplicateResourceConflict(resp.StatusCode, bodyStr) {
		pid, ferr := lbc.findPoolIDForListener(ctx, token, req.Pool.ListenerID)
		if ferr != nil {
			return resource.CreatePoolResponse{}, fmt.Errorf("create pool conflict but could not resolve existing pool: %w (body: %s)", ferr, bodyStr)
		}
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"listenerID": req.Pool.ListenerID,
			"poolID":     pid,
		}).Info("pool already exists for listener, reusing id")
		return resource.CreatePoolResponse{Pool: resource.CreatePool{ID: pid}}, nil
	}

	lbc.withLog(ctx).WithFields(logrus.Fields{
		"listenerID": req.Pool.ListenerID,
		"statusCode": resp.StatusCode,
		"error":      bodyStr,
	}).Error("failed to create pool")
	return resource.CreatePoolResponse{}, fmt.Errorf("failed to create pool, status code: %v, error msg: %v", resp.StatusCode, bodyStr)
}

func (lbc *loadbalancerService) CreateMember(ctx context.Context, authToken, poolID string, req request.AddMemberRequest) error {
	token := strings.Clone(authToken)
	data, err := json.Marshal(req)
	if err != nil {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"poolID": poolID,
		}).WithError(err).Error("failed to marshal request")
		return err
	}

	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s/%s/members", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, constants.CreateMemberPath, poolID), bytes.NewBuffer(data))
	if err != nil {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"poolID": poolID,
		}).WithError(err).Error("failed to create request")
		return err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)
	r.Header.Add("Content-Type", "application/json")

	resp, err := lbc.client.Do(r)
	if err != nil {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"poolID": poolID,
		}).WithError(err).Error("failed to send request")
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated {
		var respDecoder resource.AddMemberResponse
		if err := json.NewDecoder(resp.Body).Decode(&respDecoder); err != nil {
			lbc.withLog(ctx).WithFields(logrus.Fields{
				"poolID": poolID,
			}).WithError(err).Error("failed to decode response")
			return err
		}
		return nil
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyStr := string(bodyBytes)
	if isOctaviaDuplicateResourceConflict(resp.StatusCode, bodyStr) {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"poolID": poolID,
		}).Info("pool member already exists, treating as success")
		return nil
	}

	lbc.withLog(ctx).WithFields(logrus.Fields{
		"poolID":     poolID,
		"statusCode": resp.StatusCode,
		"error":      bodyStr,
	}).Error("failed to create member")
	return fmt.Errorf("failed to create member, status code: %v, error msg: %v", resp.StatusCode, bodyStr)
}

func (lbc *loadbalancerService) ListListener(ctx context.Context, authToken, listenerID string) (resource.ListListenerResponse, error) {
	token := strings.Clone(authToken)
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, constants.ListenersPath, listenerID), nil)
	if err != nil {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"listenerID": listenerID,
		}).WithError(err).Error("failed to create request")
		return resource.ListListenerResponse{}, err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)
	r.Header.Add("Content-Type", "application/json")

	resp, err := lbc.client.Do(r)
	if err != nil {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"listenerID": listenerID,
		}).WithError(err).Error("failed to send request")
		return resource.ListListenerResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"listenerID": listenerID,
			"statusCode": resp.StatusCode,
			"status":     resp.Status,
		}).Error("failed to list listener")
		return resource.ListListenerResponse{}, fmt.Errorf("failed to list listener, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.ListListenerResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"listenerID": listenerID,
		}).WithError(err).Error("failed to decode response")
		return resource.ListListenerResponse{}, err
	}

	return respDecoder, nil
}

func poolDetailMatchesListener(p resource.PoolDetail, listenerID string) bool {
	if listenerID == "" {
		return false
	}
	if p.ListenerID == listenerID {
		return true
	}
	for _, l := range p.Listeners {
		if l.ID == listenerID {
			return true
		}
	}
	return false
}

func (lbc *loadbalancerService) getPool(ctx context.Context, authToken, poolID string) (resource.PoolDetail, error) {
	token := strings.Clone(authToken)
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, constants.ListenerPoolPath, poolID), nil)
	if err != nil {
		return resource.PoolDetail{}, err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)
	r.Header.Add("Content-Type", "application/json")
	resp, err := lbc.client.Do(r)
	if err != nil {
		return resource.PoolDetail{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return resource.PoolDetail{}, fmt.Errorf("get pool %s: status %d: %s", poolID, resp.StatusCode, string(b))
	}
	var out resource.GetPoolResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return resource.PoolDetail{}, err
	}
	return out.Pool, nil
}

func (lbc *loadbalancerService) findListenerIDByLoadBalancerAndPort(ctx context.Context, authToken, lbID, protocol string, port int) (string, error) {
	ids, err := lbc.GetLoadBalancerListeners(ctx, authToken, lbID)
	if err != nil {
		return "", err
	}
	for _, lid := range ids.Listeners {
		lr, err := lbc.ListListener(ctx, authToken, lid)
		if err != nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(lr.Listener.Protocol), strings.TrimSpace(protocol)) && lr.Listener.ProtocolPort == port {
			return lid, nil
		}
	}
	return "", fmt.Errorf("no listener on load balancer %s for %s:%d", lbID, protocol, port)
}

func (lbc *loadbalancerService) findPoolIDForListener(ctx context.Context, authToken, listenerID string) (string, error) {
	lr, err := lbc.ListListener(ctx, authToken, listenerID)
	if err != nil {
		return "", err
	}
	lbID := strings.TrimSpace(lr.Listener.LoadbalancerID)
	if lbID == "" {
		return "", fmt.Errorf("listener %s has no loadbalancer_id", listenerID)
	}
	pools, err := lbc.GetLoadBalancerPools(ctx, authToken, lbID)
	if err != nil {
		return "", err
	}
	for _, pid := range pools.Pools {
		detail, err := lbc.getPool(ctx, authToken, pid)
		if err != nil {
			continue
		}
		if poolDetailMatchesListener(detail, listenerID) {
			return pid, nil
		}
	}
	return "", fmt.Errorf("no pool linked to listener %s", listenerID)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// pollIntervals: start fast (catch quick transitions), cap backoff to avoid hammering Octavia.
const (
	lbPollInitialInterval = 2 * time.Second
	lbPollMaxInterval     = 10 * time.Second
	lbPollOverallDeadline = 20 * time.Minute
)

// isOctaviaDuplicateResourceConflict is true for 409 responses where the resource already exists (worker retry after partial create).
// Does not match "immutable" LB state — those must keep failing until ONLINE.
func isOctaviaDuplicateResourceConflict(statusCode int, body string) bool {
	if statusCode != http.StatusConflict {
		return false
	}
	b := strings.ToLower(body)
	if strings.Contains(b, "immutable") {
		return false
	}
	if strings.Contains(b, "already has a health monitor") {
		return true
	}
	if strings.Contains(b, "member") && (strings.Contains(b, "already") || strings.Contains(b, "duplicate")) {
		return true
	}
	if strings.Contains(b, "already exists") {
		return true
	}
	if strings.Contains(b, "duplicate") {
		return true
	}
	return false
}

func (lbc *loadbalancerService) CheckLoadBalancerStatus(ctx context.Context, authToken, loadBalancerID string) (resource.ListLoadBalancerResponse, error) {
	token := strings.Clone(authToken)
	deadline := time.Now().Add(lbPollOverallDeadline)
	interval := lbPollInitialInterval
	for attempt := 1; ; attempt++ {
		if time.Now().After(deadline) {
			return resource.ListLoadBalancerResponse{}, fmt.Errorf("timeout waiting for load balancer provisioning ACTIVE")
		}
		listLBResp, err := lbc.ListLoadBalancer(ctx, token, loadBalancerID)
		if err != nil {
			lbc.withLog(ctx).WithFields(logrus.Fields{
				"loadBalancerID": loadBalancerID,
			}).WithError(err).Error("failed to list load balancer")
			return resource.ListLoadBalancerResponse{}, err
		}
		if listLBResp.LoadBalancer.ProvisioningStatus == LoadBalancerStatusError {
			return resource.ListLoadBalancerResponse{}, fmt.Errorf("failed to create load balancer, provisioning status is ERROR")
		}
		if listLBResp.LoadBalancer.ProvisioningStatus == LoadBalancerStatusActive {
			return resource.ListLoadBalancerResponse{}, nil
		}
		if attempt == 1 || attempt%12 == 0 {
			lbc.withLog(ctx).WithFields(logrus.Fields{
				"loadBalancerID": loadBalancerID,
				"provisioning":   listLBResp.LoadBalancer.ProvisioningStatus,
				"attempt":        attempt,
			}).Info("waiting for load balancer ACTIVE")
		}
		if err := sleepCtx(ctx, interval); err != nil {
			return resource.ListLoadBalancerResponse{}, err
		}
		if interval < lbPollMaxInterval {
			n := interval * 3 / 2
			if n > lbPollMaxInterval {
				interval = lbPollMaxInterval
			} else {
				interval = n
			}
		}
	}
}

func (lbc *loadbalancerService) CreateHealthHTTPMonitor(ctx context.Context, authToken string, req request.CreateHealthMonitorHTTPRequest) error {
	token := strings.Clone(authToken)
	data, err := json.Marshal(req)
	if err != nil {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"poolID": req.HealthMonitor.PoolID,
		}).WithError(err).Error("failed to marshal request")
		return err
	}
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, constants.HealthMonitorPath), bytes.NewBuffer(data))
	if err != nil {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"poolID": req.HealthMonitor.PoolID,
		}).WithError(err).Error("failed to create request")
		return err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)
	r.Header.Add("Content-Type", "application/json")

	resp, err := lbc.client.Do(r)
	if err != nil {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"poolID": req.HealthMonitor.PoolID,
		}).WithError(err).Error("failed to send request")
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated {
		var respDecoder resource.CreateHealthMonitorResponse
		if err := json.NewDecoder(resp.Body).Decode(&respDecoder); err != nil {
			lbc.withLog(ctx).WithFields(logrus.Fields{
				"poolID": req.HealthMonitor.PoolID,
			}).WithError(err).Error("failed to decode response")
			return err
		}
		return nil
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyStr := string(bodyBytes)
	if isOctaviaDuplicateResourceConflict(resp.StatusCode, bodyStr) {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"poolID": req.HealthMonitor.PoolID,
		}).Info("health monitor already exists on pool, treating as success")
		return nil
	}

	lbc.withLog(ctx).WithFields(logrus.Fields{
		"poolID":     req.HealthMonitor.PoolID,
		"statusCode": resp.StatusCode,
		"status":     resp.Status,
		"error":      bodyStr,
	}).Error("failed to create health monitor")
	return fmt.Errorf("failed to create health monitor, status code: %v, error msg: %v", resp.StatusCode, bodyStr)
}

func (lbc *loadbalancerService) CreateHealthTCPMonitor(ctx context.Context, authToken string, req request.CreateHealthMonitorTCPRequest) error {
	token := strings.Clone(authToken)
	data, err := json.Marshal(req)
	if err != nil {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"poolID": req.HealthMonitor.PoolID,
		}).WithError(err).Error("failed to marshal request")
		return err
	}
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, constants.HealthMonitorPath), bytes.NewBuffer(data))
	if err != nil {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"poolID": req.HealthMonitor.PoolID,
		}).WithError(err).Error("failed to create request")
		return err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)
	r.Header.Add("Content-Type", "application/json")

	resp, err := lbc.client.Do(r)
	if err != nil {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"poolID": req.HealthMonitor.PoolID,
		}).WithError(err).Error("failed to send request")
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated {
		var respDecoder resource.CreateHealthMonitorResponse
		if err := json.NewDecoder(resp.Body).Decode(&respDecoder); err != nil {
			lbc.withLog(ctx).WithFields(logrus.Fields{
				"poolID": req.HealthMonitor.PoolID,
			}).WithError(err).Error("failed to decode response")
			return err
		}
		return nil
	}

	bodyBytes, _ := io.ReadAll(resp.Body)
	bodyStr := string(bodyBytes)
	if isOctaviaDuplicateResourceConflict(resp.StatusCode, bodyStr) {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"poolID": req.HealthMonitor.PoolID,
		}).Info("health monitor already exists on pool, treating as success")
		return nil
	}

	lbc.withLog(ctx).WithFields(logrus.Fields{
		"poolID":     req.HealthMonitor.PoolID,
		"statusCode": resp.StatusCode,
		"status":     resp.Status,
		"error":      bodyStr,
	}).Error("failed to create health monitor")
	return fmt.Errorf("failed to create health monitor, status code: %v, error msg: %v", resp.StatusCode, bodyStr)
}

func (lbc *loadbalancerService) CheckLoadBalancerOperationStatus(ctx context.Context, authToken, loadBalancerID string) (resource.ListLoadBalancerResponse, error) {
	token := strings.Clone(authToken)
	deadline := time.Now().Add(lbPollOverallDeadline)
	interval := lbPollInitialInterval
	for attempt := 1; ; attempt++ {
		if time.Now().After(deadline) {
			return resource.ListLoadBalancerResponse{}, fmt.Errorf("timeout waiting for load balancer operating ONLINE")
		}
		listLBResp, err := lbc.ListLoadBalancer(ctx, token, loadBalancerID)
		if err != nil {
			lbc.withLog(ctx).WithFields(logrus.Fields{
				"loadBalancerID": loadBalancerID,
			}).WithError(err).Error("failed to list load balancer")
			return resource.ListLoadBalancerResponse{}, err
		}
		if listLBResp.LoadBalancer.OperatingStatus == "ONLINE" {
			return resource.ListLoadBalancerResponse{}, nil
		}
		if attempt == 1 || attempt%12 == 0 {
			lbc.withLog(ctx).WithFields(logrus.Fields{
				"loadBalancerID":  loadBalancerID,
				"operatingStatus": listLBResp.LoadBalancer.OperatingStatus,
				"attempt":         attempt,
			}).Info("waiting for load balancer ONLINE")
		}
		if err := sleepCtx(ctx, interval); err != nil {
			return resource.ListLoadBalancerResponse{}, err
		}
		if interval < lbPollMaxInterval {
			n := interval * 3 / 2
			if n > lbPollMaxInterval {
				interval = lbPollMaxInterval
			} else {
				interval = n
			}
		}
	}
}

func (lbc *loadbalancerService) GetLoadBalancerPools(ctx context.Context, authToken, loadBalancerID string) (resource.GetLoadBalancerPoolsResponse, error) {
	token := strings.Clone(authToken)
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, constants.LoadBalancerPath, loadBalancerID), nil)
	if err != nil {
		lbc.withLog(ctx).WithError(err).Error("failed to create request")
		return resource.GetLoadBalancerPoolsResponse{}, err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)

	resp, err := lbc.client.Do(r)
	if err != nil {
		lbc.withLog(ctx).WithError(err).Error("failed to send request")
		return resource.GetLoadBalancerPoolsResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"statusCode": resp.StatusCode,
			"status":     resp.Status,
		}).Error("failed to list load balancer")
		return resource.GetLoadBalancerPoolsResponse{}, fmt.Errorf("failed to list load balancer, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		lbc.withLog(ctx).WithError(err).Error("failed to read response body")
		return resource.GetLoadBalancerPoolsResponse{}, err
	}
	var respdata map[string]map[string]interface{}
	err = json.Unmarshal([]byte(body), &respdata)
	if err != nil {
		lbc.withLog(ctx).WithError(err).Error("failed to unmarshal response body")
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
				lbc.withLog(ctx).WithFields(logrus.Fields{
					"loadBalancerID": loadBalancerID,
				}).WithError(err).Error("failed to get pool id")
				return resource.GetLoadBalancerPoolsResponse{}, fmt.Errorf("failed to get pool id")
			}
		} else {
			lbc.withLog(ctx).WithFields(logrus.Fields{
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
		lbc.withLog(ctx).WithError(err).Error("failed to create request")
		return err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)

	resp, err := lbc.client.Do(r)
	if err != nil {
		lbc.withLog(ctx).WithError(err).Error("failed to send request")
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"statusCode": resp.StatusCode,
			"status":     resp.Status,
		}).Error("failed to delete load balancer pool")
		return fmt.Errorf("failed to delete load balancer pool, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}
	return nil
}

func (lbc *loadbalancerService) CheckLoadBalancerDeletingPools(ctx context.Context, authToken, poolID string) error {
	token := strings.Clone(authToken)
	deadline := time.Now().Add(lbPollOverallDeadline)
	interval := lbPollInitialInterval
	for attempt := 1; ; attempt++ {
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for load balancer pool deletion")
		}
		r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, constants.ListenerPoolPath, poolID), nil)
		if err != nil {
			lbc.withLog(ctx).WithFields(logrus.Fields{
				"poolID": poolID,
			}).WithError(err).Error("failed to create request")
			return err
		}
		r.Header = make(http.Header)
		r.Header.Add("X-Auth-Token", token)

		resp, err := lbc.client.Do(r)
		if err != nil {
			lbc.withLog(ctx).WithFields(logrus.Fields{
				"poolID": poolID,
			}).WithError(err).Error("failed to send request")
			return err
		}
		func() {
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, resp.Body)
		}()

		if resp.StatusCode == http.StatusNotFound {
			return nil
		}
		if attempt == 1 || attempt%12 == 0 {
			lbc.withLog(ctx).WithFields(logrus.Fields{
				"poolID": poolID, "attempt": attempt, "statusCode": resp.StatusCode,
			}).Info("waiting for load balancer pool deletion")
		}
		if err := sleepCtx(ctx, interval); err != nil {
			return err
		}
		if interval < lbPollMaxInterval {
			n := interval * 3 / 2
			if n > lbPollMaxInterval {
				interval = lbPollMaxInterval
			} else {
				interval = n
			}
		}
	}
}
func (lbc *loadbalancerService) GetLoadBalancerListeners(ctx context.Context, authToken, loadBalancerID string) (resource.GetLoadBalancerListenersResponse, error) {
	token := strings.Clone(authToken)
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, constants.LoadBalancerPath, loadBalancerID), nil)
	if err != nil {
		lbc.withLog(ctx).WithError(err).Error("failed to create request")
		return resource.GetLoadBalancerListenersResponse{}, err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)

	resp, err := lbc.client.Do(r)
	if err != nil {
		lbc.withLog(ctx).WithError(err).Error("failed to send request")
		return resource.GetLoadBalancerListenersResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"statusCode": resp.StatusCode,
			"status":     resp.Status,
		}).Error("failed to list load balancer")
		return resource.GetLoadBalancerListenersResponse{}, fmt.Errorf("failed to list load balancer, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		lbc.withLog(ctx).WithError(err).Error("failed to read response body")
		return resource.GetLoadBalancerListenersResponse{}, err
	}
	var respdata map[string]map[string]interface{}
	err = json.Unmarshal([]byte(body), &respdata)
	if err != nil {
		lbc.withLog(ctx).WithError(err).Error("failed to unmarshal response body")
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
				lbc.withLog(ctx).WithError(err).Error("failed to get listener id")
				return resource.GetLoadBalancerListenersResponse{}, fmt.Errorf("failed to get listener id")
			}
		} else {
			lbc.withLog(ctx).WithError(err).Error("failed to get listener id")
			return resource.GetLoadBalancerListenersResponse{}, fmt.Errorf("failed to get listener id")
		}
	}

	return respListeners, nil
}

func (lbc *loadbalancerService) DeleteLoadbalancerListeners(ctx context.Context, authToken, listenerID string) error {
	token := strings.Clone(authToken)
	r, err := http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, constants.ListenersPath, listenerID), nil)
	if err != nil {
		lbc.withLog(ctx).WithError(err).Error("failed to create request")
		return err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)

	resp, err := lbc.client.Do(r)
	if err != nil {
		lbc.withLog(ctx).WithError(err).Error("failed to send request")
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"statusCode": resp.StatusCode,
			"status":     resp.Status,
		}).Error("failed to delete load balancer listener")
		return fmt.Errorf("failed to delete load balancer listener, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}
	return nil
}

func (lbc *loadbalancerService) CheckLoadBalancerDeletingListeners(ctx context.Context, authToken, listenerID string) error {
	token := strings.Clone(authToken)
	deadline := time.Now().Add(lbPollOverallDeadline)
	interval := lbPollInitialInterval
	for attempt := 1; ; attempt++ {
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for load balancer listener deletion")
		}
		r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, constants.ListenersPath, listenerID), nil)
		if err != nil {
			lbc.withLog(ctx).WithError(err).Error("failed to create request")
			return err
		}
		r.Header = make(http.Header)
		r.Header.Add("X-Auth-Token", token)

		resp, err := lbc.client.Do(r)
		if err != nil {
			lbc.withLog(ctx).WithError(err).Error("failed to send request")
			return err
		}
		func() {
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, resp.Body)
		}()

		if resp.StatusCode == http.StatusNotFound {
			return nil
		}
		if attempt == 1 || attempt%12 == 0 {
			lbc.withLog(ctx).WithFields(logrus.Fields{
				"listenerID": listenerID, "attempt": attempt, "statusCode": resp.StatusCode,
			}).Info("waiting for load balancer listener deletion")
		}
		if err := sleepCtx(ctx, interval); err != nil {
			return err
		}
		if interval < lbPollMaxInterval {
			n := interval * 3 / 2
			if n > lbPollMaxInterval {
				interval = lbPollMaxInterval
			} else {
				interval = n
			}
		}
	}
}

func (lbc *loadbalancerService) DeleteLoadbalancer(ctx context.Context, authToken, loadBalancerID string) error {
	token := strings.Clone(authToken)
	r, err := http.NewRequest("DELETE", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, constants.LoadBalancerPath, loadBalancerID), nil)
	if err != nil {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"loadBalancerID": loadBalancerID,
		}).WithError(err).Error("failed to create request")
		return err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)

	resp, err := lbc.client.Do(r)
	if err != nil {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"loadBalancerID": loadBalancerID,
		}).WithError(err).Error("failed to send request")
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		lbc.withLog(ctx).WithFields(logrus.Fields{
			"loadBalancerID": loadBalancerID,
			"statusCode":     resp.StatusCode,
			"status":         resp.Status,
		}).Error("failed to delete load balancer")
		return fmt.Errorf("failed to delete load balancer, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	return nil
}

func (lbc *loadbalancerService) WaitForLoadBalancerDeleted(ctx context.Context, authToken, loadBalancerID string) error {
	token := strings.Clone(authToken)
	deadline := time.Now().Add(lbPollOverallDeadline)
	interval := lbPollInitialInterval
	for attempt := 1; ; attempt++ {
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for load balancer deletion")
		}
		r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().LoadBalancerEndpoint, constants.LoadBalancerPath, loadBalancerID), nil)
		if err != nil {
			lbc.withLog(ctx).WithFields(logrus.Fields{
				"loadBalancerID": loadBalancerID,
			}).WithError(err).Error("failed to create request")
			return err
		}
		r.Header = make(http.Header)
		r.Header.Add("X-Auth-Token", token)

		resp, err := lbc.client.Do(r)
		if err != nil {
			lbc.withLog(ctx).WithFields(logrus.Fields{
				"loadBalancerID": loadBalancerID,
			}).WithError(err).Error("failed to send request")
			return err
		}
		func() {
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, resp.Body)
		}()

		if resp.StatusCode == http.StatusNotFound {
			return nil
		}
		if attempt == 1 || attempt%12 == 0 {
			lbc.withLog(ctx).WithFields(logrus.Fields{
				"loadBalancerID": loadBalancerID, "attempt": attempt, "statusCode": resp.StatusCode,
			}).Info("waiting for load balancer resource deletion")
		}
		if err := sleepCtx(ctx, interval); err != nil {
			return err
		}
		if interval < lbPollMaxInterval {
			n := interval * 3 / 2
			if n > lbPollMaxInterval {
				interval = lbPollMaxInterval
			} else {
				interval = n
			}
		}
	}
}
