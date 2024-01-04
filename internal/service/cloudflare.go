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
)

type ICloudflareService interface {
	AddDNSRecordToCloudflare(ctx context.Context, loadBalancerIP, loadBalancerSubdomainHash, clusterName string) (resource.AddDNSRecordResponse, error)
	DeleteDNSRecordFromCloudflare(ctx context.Context, dnsRecordID string) error
}

type cloudflareService struct {
	logger *logrus.Logger
}

func NewCloudflareService(logger *logrus.Logger) ICloudflareService {
	return &cloudflareService{
		logger: logger,
	}
}

func (cf *cloudflareService) AddDNSRecordToCloudflare(ctx context.Context, loadBalancerIP, loadBalancerSubdomainHash, clusterName string) (resource.AddDNSRecordResponse, error) {
	addDNSRecordCFRequest := &request.AddDNSRecordCFRequest{
		Content: loadBalancerIP,
		Name:    fmt.Sprintf("%s.%s", loadBalancerSubdomainHash, config.GlobalConfig.GetCloudflareConfig().Domain),
		Proxied: false,
		Type:    "A",
		Comment: clusterName,
		Tags:    []string{},
		TTL:     3600,
	}
	data, err := json.Marshal(addDNSRecordCFRequest)
	if err != nil {
		cf.logger.Errorf("failed to marshal request, error: %v", err)
		return resource.AddDNSRecordResponse{}, err
	}

	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s/dns_records", cloudflareEndpoint, config.GlobalConfig.GetCloudflareConfig().ZoneID), bytes.NewBuffer(data))
	if err != nil {
		cf.logger.Errorf("failed to create request, error: %v", err)
		return resource.AddDNSRecordResponse{}, err
	}

	r.Header.Add("Authorization", fmt.Sprintf("Bearer %s", config.GlobalConfig.GetCloudflareConfig().AuthToken))
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		cf.logger.Errorf("failed to send request, error: %v", err)
		return resource.AddDNSRecordResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		cf.logger.Errorf("failed to add dns record, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return resource.AddDNSRecordResponse{}, fmt.Errorf("failed to add dns record, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.AddDNSRecordResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		cf.logger.Errorf("failed to decode response, error: %v", err)
		return resource.AddDNSRecordResponse{}, err
	}

	return respDecoder, nil
}

func (cf *cloudflareService) DeleteDNSRecordFromCloudflare(ctx context.Context, dnsRecordID string) error {
	r, err := http.NewRequest("DELETE", fmt.Sprintf("%s/%s/dns_records/%s", cloudflareEndpoint, config.GlobalConfig.GetCloudflareConfig().ZoneID, dnsRecordID), nil)
	if err != nil {
		cf.logger.Errorf("failed to create request, error: %v", err)
		return err
	}

	r.Header.Add("Authorization", fmt.Sprintf("Bearer %s", config.GlobalConfig.GetCloudflareConfig().AuthToken))
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		cf.logger.Errorf("failed to send request, error: %v", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		cf.logger.Errorf("failed to delete dns record, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return fmt.Errorf("failed to delete dns record, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	return nil
}
