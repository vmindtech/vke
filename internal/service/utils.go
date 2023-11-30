package service

import (
	"bytes"
	"encoding/base64"
	"math/rand"
	"text/template"
	"time"
)

func GenerateUserDataFromTemplate(initiliazeFlag, rke2AgentType, rke2Token, serverAddress, kubeVersion string) (string, error) {
	shFile := "scripts/rke2-init-sh.tpl"
	t, err := template.ParseFiles(shFile)
	if err != nil {
		return "", err
	}

	var tpl bytes.Buffer

	if err := t.Execute(&tpl, map[string]string{
		"initiliazeFlag": initiliazeFlag,
		"rke2AgentType":  rke2AgentType,
		"rke2Token":      rke2Token,
		"serverAddress":  serverAddress,
		"kubeVersion":    kubeVersion,
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
