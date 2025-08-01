package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/vmindtech/vke/config"
	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/pkg/constants"
)

type IIdentityService interface {
	CheckAuthToken(ctx context.Context, authToken, projectID string) error
	CreateApplicationCredential(ctx context.Context, clusterUUID, authToken string) (resource.CreateApplicationCredentialResponse, error)
	DeleteApplicationCredential(ctx context.Context, authToken, projectID string) error
}

type identityService struct {
	logger *logrus.Logger
	client http.Client
}

func NewIdentityService(logger *logrus.Logger) IIdentityService {
	return &identityService{
		logger: logger,
		client: CreateHTTPClient(),
	}
}

func (i *identityService) CheckAuthToken(ctx context.Context, authToken, projectUUID string) error {
	token := strings.Clone(authToken)
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s/%s", config.GlobalConfig.GetEndpointsConfig().IdentityEndpoint, constants.ProjectPath, projectUUID), nil)
	if err != nil {
		i.logger.WithError(err).Error("failed to create request")
		return err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)

	resp, err := i.client.Do(r)
	if err != nil {
		i.logger.WithError(err).Error("failed to send request")
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
		i.logger.WithError(err).Error("failed to decode response")
		return err
	}

	if respDecoder.Project.ID != projectUUID {
		i.logger.Error("failed to check auth token, project id mismatch")
		return fmt.Errorf("failed to check auth token, project id mismatch")
	}

	return nil
}
func (i *identityService) GetTokenDetail(ctx context.Context, authToken string) (string, error) {
	token := strings.Clone(authToken)
	r, err := http.NewRequest("GET", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().IdentityEndpoint, constants.TokenPath), nil)
	if err != nil {
		i.logger.WithError(err).Error("failed to create request")
		return "", err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)
	r.Header.Add("X-Subject-Token", token)

	resp, err := i.client.Do(r)
	if err != nil {
		i.logger.WithError(err).Error("failed to send request")
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
		i.logger.WithError(err).Error("failed to decode response")
		return "", err
	}
	return respDecoder.Token.User.ID, nil
}

func (i *identityService) CreateApplicationCredential(ctx context.Context, clusterUUID, authToken string) (resource.CreateApplicationCredentialResponse, error) {
	token := strings.Clone(authToken)
	GenerateSecret := uuid.New().String()
	createApplicationCredentialReq := &request.CreateApplicationCredentialRequest{
		ApplicationCredential: request.ApplicationCredential{
			Name:        fmt.Sprintf("credential-for-vke-%s-cluster", clusterUUID),
			Secret:      GenerateSecret,
			Description: "Application Credential for VKE cluster",
			Roles: []map[string]string{
				{"name": config.GlobalConfig.GetOpenstackRolesConfig().OpenstackLoadbalancerRole},
				{"name": config.GlobalConfig.GetOpenstackRolesConfig().OpenstackMemberOrUserRole},
			},
		},
	}
	data, err := json.Marshal(createApplicationCredentialReq)
	if err != nil {
		i.logger.WithError(err).Error("failed to marshal request")
		return resource.CreateApplicationCredentialResponse{}, err
	}
	getUserID, err := i.GetTokenDetail(ctx, authToken)
	if err != nil {
		i.logger.WithError(err).Error("failed to get user id")
		return resource.CreateApplicationCredentialResponse{}, err
	}

	applicationCredentialPath := fmt.Sprintf("v3/users/%s/application_credentials", getUserID)
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().IdentityEndpoint, applicationCredentialPath), bytes.NewBuffer(data))
	if err != nil {
		i.logger.WithError(err).Error("failed to create request")
		return resource.CreateApplicationCredentialResponse{}, err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)
	r.Header.Add("Content-Type", "application/json")

	resp, err := i.client.Do(r)
	if err != nil {
		i.logger.WithError(err).Error("failed to send request")
		return resource.CreateApplicationCredentialResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return resource.CreateApplicationCredentialResponse{}, fmt.Errorf("failed to create application credential, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		i.logger.WithError(err).Error("failed to read response body")
		return resource.CreateApplicationCredentialResponse{}, err
	}
	var respDecoder resource.CreateApplicationCredentialResponse
	err = json.Unmarshal([]byte(body), &respDecoder)
	if err != nil {
		i.logger.WithError(err).Error("failed to decode response")
		return resource.CreateApplicationCredentialResponse{}, err
	}
	return respDecoder, nil

}

func (i *identityService) DeleteApplicationCredential(ctx context.Context, authToken, applicationCredentialID string) error {
	token := strings.Clone(authToken)
	getUserID, err := i.GetTokenDetail(ctx, token)
	if err != nil {
		i.logger.WithError(err).Error("failed to get user id")
		return err
	}
	applicationCredentialPath := fmt.Sprintf("v3/users/%s/application_credentials/%v", getUserID, applicationCredentialID)
	r, err := http.NewRequest("DELETE", fmt.Sprintf("%s/%s", config.GlobalConfig.GetEndpointsConfig().IdentityEndpoint, applicationCredentialPath), nil)
	if err != nil {
		i.logger.WithError(err).Error("failed to create request")
		return err
	}
	r.Header = make(http.Header)
	r.Header.Add("X-Auth-Token", token)

	resp, err := i.client.Do(r)
	if err != nil {
		i.logger.WithError(err).Error("failed to send request")
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("failed to delete application credential, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
	}
	return nil
}
