package service

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"math/rand"
	"net/http"
	"text/template"
	"time"

	"gorm.io/datatypes"
)

func GenerateUserDataFromTemplate(
	initiliazeFlag,
	rke2AgentType,
	rke2Token,
	serverAddress,
	kubeVersion,
	clusterName,
	clusterUUID,
	projectUUID,
	vkeAPIEndpoint,
	token,
	vkeAgentVersion,
	rke2NodeLabel,
	rke2NodeTaints,
	vkeCloudAuthURL,
	clusterAutoscalerVersion,
	cloudProviderVkeVersion,
	applicationCredentialID,
	applicationCredentialKey,
	clusterAgentVersion,
	loadBalancerFloatingNetworkID string,
) (string, error) {
	shFile := "scripts/rke2-init-sh.tpl"
	t, err := template.ParseFiles(shFile)
	if err != nil {
		return "", err
	}

	var tpl bytes.Buffer

	if err := t.Execute(&tpl, map[string]string{
		"initiliazeFlag":                initiliazeFlag,
		"rke2AgentType":                 rke2AgentType,
		"rke2Token":                     rke2Token,
		"serverAddress":                 serverAddress,
		"kubeVersion":                   kubeVersion,
		"clusterName":                   clusterName,
		"clusterUUID":                   clusterUUID,
		"projectUUID":                   projectUUID,
		"vkeAPIEndpoint":                vkeAPIEndpoint,
		"authToken":                     token,
		"vkeAgentVersion":               vkeAgentVersion,
		"rke2NodeLabel":                 rke2NodeLabel,
		"vkeCloudAuthURL":               vkeCloudAuthURL,
		"clusterAutoscalerVersion":      clusterAutoscalerVersion,
		"cloudProviderVkeVersion":       cloudProviderVkeVersion,
		"applicationCredentialID":       applicationCredentialID,
		"applicationCredentialKey":      applicationCredentialKey,
		"clusterAgentVersion":           clusterAgentVersion,
		"loadBalancerFloatingNetworkID": loadBalancerFloatingNetworkID,
		"rke2NodeTaints":                rke2NodeTaints,
	}); err != nil {
		return "", err
	}

	return tpl.String(), nil
}

func Base64Encoder(data string) string {
	return base64.StdEncoding.EncodeToString([]byte(data))
}

func GetRandomStringFromArray(a []string) string {
	rand.Seed(time.Now().UnixNano())
	i := rand.Intn(len(a))
	return a[i]
}

func IsValidBase64(s string) bool {
	_, err := base64.StdEncoding.DecodeString(s)
	return err == nil
}

func ConvertDataJSONtoStringArray(jsonData datatypes.JSON) []string {
	result := []string{}
	_ = json.Unmarshal([]byte(jsonData), &result)

	return result
}

func DeleteItemFromArray(a []string, item string) []string {
	for i, v := range a {
		if v == item {
			return append(a[:i], a[i+1:]...)
		}
	}
	return a
}

func CreateHTTPClient() http.Client {
	t := &http.Transport{
		ForceAttemptHTTP2:     true,
		MaxIdleConnsPerHost:   100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		WriteBufferSize:       64 * 1024,
		ReadBufferSize:        64 * 1024,
	}

	return http.Client{
		Transport: t,
		Timeout:   time.Second * 30,
	}
}
