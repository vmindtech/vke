#!/bin/bash
curl -LO -k https://github.com/vmindtech/vke-agent/releases/download/v0.0.2/vke-agent_v0.0.2_linux_amd64.tar.gz
systemctl stop ufw
systemctl disable ufw
tar -xvf vke-agent_v0.0.2_linux_amd64.tar.gz
chmod +x vke-agent
./vke-agent -initialize={{.initiliazeFlag}} -rke2AgentType="{{.rke2AgentType}}" -rke2Token="{{.rke2Token}}" -serverAddress="{{.serverAddress}}" -kubeversion='{{.kubeVersion}}' -tlsSan="{{.serverAddress}}" -rke2ClusterName="{{.clusterName}}" -rke2ClusterUUID="{{.clusterUUID}}" -rke2AgentVKEAPIEndpoint="{{.vkeAPIEndpoint}}" -rke2AgentVKEAPIAuthToken="{{.authToken}}"