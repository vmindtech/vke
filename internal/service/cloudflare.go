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
	DeleteDNSRecord(ctx context.Context, recordID string) error
}

type cloudflareService struct {
	logger *logrus.Logger
	client *http.Client
}

func NewCloudflareService(logger *logrus.Logger) ICloudflareService {
	return &cloudflareService{
		logger: logger,
		client: CreateHTTPClient(),
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
		cf.logger.WithError(err).WithFields(logrus.Fields{
			"loadBalancerSubdomainHash": loadBalancerSubdomainHash,
			"clusterName":               clusterName,
		}).Error("failed to marshal request")
		return resource.AddDNSRecordResponse{}, err
	}

	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s/dns_records", cloudflareEndpoint, config.GlobalConfig.GetCloudflareConfig().ZoneID), bytes.NewBuffer(data))
	if err != nil {
		cf.logger.WithError(err).WithFields(logrus.Fields{
			"loadBalancerSubdomainHash": loadBalancerSubdomainHash,
			"clusterName":               clusterName,
		}).Error("failed to create request")
		return resource.AddDNSRecordResponse{}, err
	}

	r.Header.Add("Authorization", fmt.Sprintf("Bearer %s", config.GlobalConfig.GetCloudflareConfig().AuthToken))
	r.Header.Add("Content-Type", "application/json")

	resp, err := cf.client.Do(r)
	if err != nil {
		cf.logger.WithError(err).WithFields(logrus.Fields{
			"loadBalancerSubdomainHash": loadBalancerSubdomainHash,
			"clusterName":               clusterName,
		}).Error("failed to send request")

		return resource.AddDNSRecordResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		cf.logger.WithFields(logrus.Fields{
			"loadBalancerSubdomainHash": loadBalancerSubdomainHash,
			"clusterName":               clusterName,
		}).Errorf("failed to add dns record, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return resource.AddDNSRecordResponse{}, fmt.Errorf("failed to add dns record, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.AddDNSRecordResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		cf.logger.WithError(err).WithFields(logrus.Fields{
			"loadBalancerSubdomainHash": loadBalancerSubdomainHash,
			"clusterName":               clusterName,
		}).Error("failed to decode response")
		return resource.AddDNSRecordResponse{}, err
	}

	return respDecoder, nil
}

func (cf *cloudflareService) DeleteDNSRecordFromCloudflare(ctx context.Context, dnsRecordID string) error {
	r, err := http.NewRequest("DELETE", fmt.Sprintf("%s/%s/dns_records/%s", cloudflareEndpoint, config.GlobalConfig.GetCloudflareConfig().ZoneID, dnsRecordID), nil)
	if err != nil {
		cf.logger.WithError(err).WithField("dnsRecordID", dnsRecordID).Error("failed to create request")
		return err
	}

	r.Header.Add("Authorization", fmt.Sprintf("Bearer %s", config.GlobalConfig.GetCloudflareConfig().AuthToken))
	r.Header.Add("Content-Type", "application/json")

	resp, err := cf.client.Do(r)
	if err != nil {
		cf.logger.WithError(err).WithField("dnsRecordID", dnsRecordID).Error("failed to send request")
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		cf.logger.WithField("dnsRecordID", dnsRecordID).Errorf("failed to delete dns record, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return fmt.Errorf("failed to delete dns record, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	return nil
}

func (cf *cloudflareService) DeleteDNSRecord(ctx context.Context, recordID string) error {
	r, err := http.NewRequest("DELETE", fmt.Sprintf("%s/%s/dns_records/%s", cloudflareEndpoint, config.GlobalConfig.GetCloudflareConfig().ZoneID, recordID), nil)
	if err != nil {
		cf.logger.WithError(err).WithField("recordID", recordID).Error("failed to create request")
		return err
	}

	r.Header.Add("Authorization", fmt.Sprintf("Bearer %s", config.GlobalConfig.GetCloudflareConfig().AuthToken))
	r.Header.Add("Content-Type", "application/json")

	resp, err := cf.client.Do(r)
	if err != nil {
		cf.logger.WithError(err).WithField("recordID", recordID).Error("failed to send request")
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		cf.logger.WithField("recordID", recordID).Errorf("failed to delete dns record, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		return fmt.Errorf("failed to delete dns record, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	return nil
}
