#!/bin/bash
curl -LO -k https://github.com/vmindtech/vke-agent/releases/download/v{{.vkeAgentVersion}}/vke-agent_v{{.vkeAgentVersion}}_linux_amd64.tar.gz
systemctl stop ufw
systemctl disable ufw
tar -xvf vke-agent_v{{.vkeAgentVersion}}_linux_amd64.tar.gz
chmod +x vke-agent
./vke-agent --initialize={{.initiliazeFlag}} --rke2AgentType={{.rke2AgentType}} --rke2Token={{.rke2Token}} --serverAddress={{.serverAddress}} --kubeversion={{.kubeVersion}} --tlsSan={{.serverAddress}} --rke2ClusterName={{.clusterName}} --rke2ClusterUUID={{.clusterUUID}} --rke2ClusterProjectUUID={{.projectUUID}} --rke2AgentVKEAPIEndpoint={{.vkeAPIEndpoint}} --rke2AgentVKEAPIAuthToken={{.authToken}} --rke2NodeLabel={{.rke2NodeLabel}} --vkeCloudAuthURL={{.vkeCloudAuthURL}} --clusterAutoscalerVersion={{.clusterAutoscalerVersion}} --cloudProviderVkeVersion={{.cloudProviderVkeVersion}} --applicationCredentialID={{.applicationCredentialID}} --applicationCredentialKey={{.applicationCredentialKey}}