package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/vmindtech/vke/config"
	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/dto/resource"
)

type IIdentityService interface {
	CheckAuthToken(ctx context.Context, authToken, projectUUID string) error
	CreateApplicationCredential(ctx context.Context, clusterUUID, authToken string) (*resource.CreateApplicationCredentialResponse, error)
	GetTokenDetail(ctx context.Context, authToken string) (string, error)
	DeleteApplicationCredential(ctx context.Context, authToken, applicationCredentialID string) error
}

type identityService struct {
	logger *logrus.Logger
}

func NewIdentityService(logger *logrus.Logger) IIdentityService {
	return &identityService{
		logger: logger,
	}
}

func (i *identityService) CheckAuthToken(ctx context.Context, authToken, projectUUID string) error {
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().IdentityEndpoint, projectPath, projectUUID), nil)
	if err != nil {
		i.logger.Errorf("failed to create request, error: %v", err)
		return err
	}
	r.Header.Add("X-Auth-Token", authToken)

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		i.logger.Errorf("failed to send request, error: %v", err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// i.logger.Errorf("failed to check auth token, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		// if err != nil {
		// 	log.Fatalln(err)
		// }
		return fmt.Errorf("failed to check auth token, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.GetProjectDetailsResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		i.logger.Errorf("failed to decode response, error: %v", err)
		return err
	}

	if respDecoder.Project.ID != projectUUID {
		i.logger.Errorf("failed to check auth token, project id mismatch")
		return fmt.Errorf("failed to check auth token, project id mismatch")
	}

	return nil
}
func (i *identityService) GetTokenDetail(ctx context.Context, authToken string) (string, error) {
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().IdentityEndpoint, tokenPath), nil)
	if err != nil {
		i.logger.Errorf("failed to create request, error: %v", err)
		return "", err
	}
	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("X-Subject-Token", authToken)

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		i.logger.Errorf("failed to send request, error: %v", err)
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// i.logger.Errorf("failed to check auth token, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		// if err != nil {
		// 	log.Fatalln(err)
		// }
		return "", fmt.Errorf("failed to check auth token, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	var respDecoder resource.GetTokenDetailsResponse

	err = json.NewDecoder(resp.Body).Decode(&respDecoder)
	if err != nil {
		i.logger.Errorf("failed to decode response, error: %v", err)
		return "", err
	}
	return respDecoder.Token.User.ID, nil
}

func (i *identityService) CreateApplicationCredential(ctx context.Context, clusterUUID, authToken string) (*resource.CreateApplicationCredentialResponse, error) {
	GenerateSecret := uuid.New().String()
	createApplicationCredentialReq := &request.CreateApplicationCredentialRequest{
		ApplicationCredential: request.ApplicationCredential{
			Name:        fmt.Sprintf("credential-for-vke-%s-cluster", clusterUUID),
			Secret:      GenerateSecret,
			Description: "Application Credential for VKE cluster",
			Roles: []map[string]string{
				{"name": "user"},
				{"name": "load-balancer_admin"},
			},
		},
	}
	data, err := json.Marshal(createApplicationCredentialReq)
	if err != nil {
		i.logger.Errorf("failed to marshal request, error: %v", err)
		return nil, err
	}
	getUserID, err := i.GetTokenDetail(ctx, authToken)
	if err != nil {
		i.logger.Errorf("failed to get user id, error: %v", err)
		return nil, err
	}

	applicationCredentialPath := fmt.Sprintf("/v3/users/%s/application_credentials", getUserID)
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().IdentityEndpoint, applicationCredentialPath), bytes.NewBuffer(data))
	if err != nil {
		i.logger.Errorf("failed to create request, error: %v", err)
		return nil, err
	}

	r.Header.Add("X-Auth-Token", authToken)
	r.Header.Add("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		i.logger.Errorf("failed to send request, error: %v", err)
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("failed to create application credential, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		i.logger.Errorf("failed to read response body, error: %v", err)
		return nil, err
	}
	var respDecoder resource.CreateApplicationCredentialResponse
	err = json.Unmarshal([]byte(body), &respDecoder)
	if err != nil {
		i.logger.Errorf("failed to decode response, error: %v", err)
		return nil, err
	}
	return &respDecoder, nil

}

func (i *identityService) DeleteApplicationCredential(ctx context.Context, authToken, applicationCredentialID string) error {
	getUserID, err := i.GetTokenDetail(ctx, authToken)
	if err != nil {
		i.logger.Errorf("failed to get user id, error: %v", err)
		return err
	}
	applicationCredentialPath := fmt.Sprintf("/v3/users/%s/application_credentials/%v", getUserID, applicationCredentialID)
	r, err := http.NewRequest("DELETE", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().IdentityEndpoint, applicationCredentialPath), nil)
	if err != nil {
		i.logger.Errorf("failed to create request, error: %v", err)
		return err
	}

	r.Header.Add("X-Auth-Token", authToken)

	client := &http.Client{}
	resp, err := client.Do(r)
	if err != nil {
		i.logger.Errorf("failed to send request, error: %v", err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("failed to delete application credential, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}
	return nil
}
