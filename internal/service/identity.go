package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/sirupsen/logrus"
	"github.com/vmindtech/vke/config"
	"github.com/vmindtech/vke/internal/dto/resource"
)

type IIdentityService interface {
	CheckAuthToken(ctx context.Context, authToken, projectUUID string) error
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
		i.logger.Errorf("failed to check auth token, status code: %v, error msg: %v", resp.StatusCode, resp.Status)
		if err != nil {
			log.Fatalln(err)
		}
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
