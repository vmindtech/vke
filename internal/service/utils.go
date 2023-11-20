package service

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"html/template"
)

func GenerateUUID(len int) string {
	b := make([]byte, len)
	_, err := rand.Read(b)
	if err != nil {
		panic(err)
	}

	uniqueString := hex.EncodeToString(b)[:len]
	return uniqueString
}

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
