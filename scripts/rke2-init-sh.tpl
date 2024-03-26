#!/bin/bash
LATEST_VERSION=$(curl -sL https://github.com/vmindtech/vke-agent/releases/latest| grep -o 'tag/v[0-9]\+\.[0-9]\+\.[0-9]\+' | awk -F '/' '{print $2}' | uniq)

if [ -z "$LATEST_VERSION" ]; then
    echo -e "\e[91mError:\e[0m Latest version not found."
    exit 1
fi

DOWNLOAD_URL="https://github.com/vmindtech/vke-agent/releases/download/${LATEST_VERSION}/vke-agent_${LATEST_VERSION}_linux_amd64.tar.gz"

curl -LO -k $DOWNLOAD_URL
systemctl stop ufw
systemctl disable ufw
tar -xvf vke-agent_${LATEST_VERSION}_linux_amd64.tar.gz
chmod +x vke-agent
./vke-agent --initialize={{.initiliazeFlag}} --rke2AgentType={{.rke2AgentType}} --rke2Token={{.rke2Token}} --serverAddress={{.serverAddress}} --kubeversion={{.kubeVersion}} --tlsSan={{.serverAddress}} --rke2ClusterName={{.clusterName}} --rke2ClusterUUID={{.clusterUUID}} --rke2AgentVKEAPIEndpoint={{.vkeAPIEndpoint}} --rke2AgentVKEAPIAuthToken={{.authToken}}