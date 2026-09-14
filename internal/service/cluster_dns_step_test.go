package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/internal/model"
)

func dnsTestRequest(access string) *request.CreateClusterRequest {
	return &request.CreateClusterRequest{ClusterName: "demo", ClusterAPIAccess: access}
}

func expectVIPLookup(m *clusterServiceMocks, lbID, vip string) {
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "load_balancer").
		Return([]model.Resource{{ResourceUUID: lbID}}, nil)
	var lbResp resource.ListLoadBalancerResponse
	lbResp.LoadBalancer.VIPAddress = vip
	m.lb.EXPECT().ListLoadBalancer(gomock.Any(), "tok", lbID).Return(lbResp, nil)
}

func TestStepEnsureDNS_AlreadyDone(t *testing.T) {
	svc, _ := newClusterServiceForTest(t)

	cluster := &model.Cluster{
		ClusterUUID:               "cid-1",
		ClusterCloudflareRecordID: "rec-1",
		ClusterEndpoint:           "hash.vke.example.com",
	}
	require.NoError(t, svc.stepEnsureDNS(context.Background(), "tok", dnsTestRequest("public"), cluster))
}

func TestStepEnsureDNS_LoadBalancerMissing(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "load_balancer").
		Return(nil, nil)

	err := svc.stepEnsureDNS(context.Background(), "tok", dnsTestRequest("private"), &model.Cluster{ClusterUUID: "cid-1"})
	require.ErrorContains(t, err, "load balancer missing")
}

func TestStepEnsureDNS_PrivateUsesVIP(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	expectVIPLookup(m, "lb-1", "10.0.0.5")
	dnsResp := resource.AddDNSRecordResponse{}
	dnsResp.Result.ID = "rec-1"
	dnsResp.Result.Name = "hash-1.vke.example.com"
	m.cloudflare.EXPECT().AddDNSRecordToCloudflare(gomock.Any(), "10.0.0.5", "hash-1", "demo").
		Return(dnsResp, nil)
	m.clusters.EXPECT().UpdateCluster(gomock.Any(), &model.Cluster{
		ClusterUUID:               "cid-1",
		ClusterEndpoint:           "hash-1.vke.example.com",
		ClusterCloudflareRecordID: "rec-1",
	}).Return(nil)

	cluster := &model.Cluster{ClusterUUID: "cid-1", ClusterSubdomainHash: "hash-1"}
	require.NoError(t, svc.stepEnsureDNS(context.Background(), "tok", dnsTestRequest("private"), cluster))
}

func TestStepEnsureDNS_PublicUsesFloatingIPFromColumn(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	expectVIPLookup(m, "lb-1", "10.0.0.5")
	var fipResp resource.CreateFloatingIPResponse
	fipResp.FloatingIP.FloatingIP = "203.0.113.10"
	m.network.EXPECT().GetFloatingIP(gomock.Any(), "tok", "fip-1").Return(fipResp, nil)
	m.cloudflare.EXPECT().AddDNSRecordToCloudflare(gomock.Any(), "203.0.113.10", "hash-1", "demo").
		Return(resource.AddDNSRecordResponse{}, nil)
	m.clusters.EXPECT().UpdateCluster(gomock.Any(), gomock.Any()).Return(nil)

	cluster := &model.Cluster{ClusterUUID: "cid-1", ClusterSubdomainHash: "hash-1", FloatingIPUUID: "fip-1"}
	require.NoError(t, svc.stepEnsureDNS(context.Background(), "tok", dnsTestRequest("public"), cluster))
}

func TestStepEnsureDNS_PublicFallsBackToResourcesRow(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	expectVIPLookup(m, "lb-1", "10.0.0.5")
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "floating_ip").
		Return([]model.Resource{{ResourceUUID: "fip-2"}}, nil)
	var fipResp resource.CreateFloatingIPResponse
	fipResp.FloatingIP.FloatingIP = "203.0.113.20"
	m.network.EXPECT().GetFloatingIP(gomock.Any(), "tok", "fip-2").Return(fipResp, nil)
	m.cloudflare.EXPECT().AddDNSRecordToCloudflare(gomock.Any(), "203.0.113.20", "hash-1", "demo").
		Return(resource.AddDNSRecordResponse{}, nil)
	m.clusters.EXPECT().UpdateCluster(gomock.Any(), gomock.Any()).Return(nil)

	cluster := &model.Cluster{ClusterUUID: "cid-1", ClusterSubdomainHash: "hash-1"}
	require.NoError(t, svc.stepEnsureDNS(context.Background(), "tok", dnsTestRequest("public"), cluster))
}

func TestStepEnsureDNS_PublicWithoutFloatingIPFails(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	expectVIPLookup(m, "lb-1", "10.0.0.5")
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "floating_ip").
		Return(nil, nil)

	cluster := &model.Cluster{ClusterUUID: "cid-1", ClusterSubdomainHash: "hash-1"}
	err := svc.stepEnsureDNS(context.Background(), "tok", dnsTestRequest("public"), cluster)
	require.ErrorContains(t, err, "floating IP not found")
}

func TestStepEnsureDNS_GeneratesSubdomainHashWhenEmpty(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	expectVIPLookup(m, "lb-1", "10.0.0.5")

	var persistedHash, usedHash string
	m.clusters.EXPECT().UpdateCluster(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, c *model.Cluster) error {
			if c.ClusterSubdomainHash != "" {
				persistedHash = c.ClusterSubdomainHash
			}
			return nil
		}).Times(2) // once for the hash, once for endpoint/record id
	m.cloudflare.EXPECT().AddDNSRecordToCloudflare(gomock.Any(), "10.0.0.5", gomock.Any(), "demo").DoAndReturn(
		func(_ context.Context, _, hash, _ string) (resource.AddDNSRecordResponse, error) {
			usedHash = hash
			return resource.AddDNSRecordResponse{}, nil
		})

	cluster := &model.Cluster{ClusterUUID: "cid-1"}
	require.NoError(t, svc.stepEnsureDNS(context.Background(), "tok", dnsTestRequest("private"), cluster))

	_, err := uuid.Parse(persistedHash)
	assert.NoError(t, err, "generated hash must be a UUID")
	assert.Equal(t, persistedHash, usedHash, "persisted and DNS-registered hash must match")
}

func TestStepEnsureDNS_CloudflareErrorPropagates(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	expectVIPLookup(m, "lb-1", "10.0.0.5")
	boom := errors.New("cloudflare 502")
	m.cloudflare.EXPECT().AddDNSRecordToCloudflare(gomock.Any(), "10.0.0.5", "hash-1", "demo").
		Return(resource.AddDNSRecordResponse{}, boom)

	cluster := &model.Cluster{ClusterUUID: "cid-1", ClusterSubdomainHash: "hash-1"}
	err := svc.stepEnsureDNS(context.Background(), "tok", dnsTestRequest("private"), cluster)
	require.ErrorIs(t, err, boom)
}
