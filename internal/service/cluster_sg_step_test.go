package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/vmindtech/vke/internal/dto/request"
	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/internal/model"
)

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }

func ingressTCPRule(sgID string, portMin, portMax int, cidr, ethertype string) resource.SecurityGroupRuleDTO {
	return resource.SecurityGroupRuleDTO{
		Direction:       "ingress",
		Ethertype:       ethertype,
		Protocol:        strPtr("tcp"),
		PortRangeMin:    intPtr(portMin),
		PortRangeMax:    intPtr(portMax),
		RemoteIPPrefix:  cidr,
		SecurityGroupID: sgID,
	}
}

func TestInferEthertypeFromRemoteCIDR(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "IPv4", inferEthertypeFromRemoteCIDR("10.0.0.0/16"))
	assert.Equal(t, "IPv6", inferEthertypeFromRemoteCIDR("2001:db8::/32"))
	assert.Equal(t, "IPv4", inferEthertypeFromRemoteCIDR(""))
}

func TestSGRulesContainIngressTCPPortRangeFromCIDR(t *testing.T) {
	t.Parallel()

	rules := []resource.SecurityGroupRuleDTO{
		ingressTCPRule("sg-1", 6443, 6443, "10.0.0.0/16", "IPv4"),
	}

	assert.True(t, sgRulesContainIngressTCPPortRangeFromCIDR(rules, 6443, 6443, "10.0.0.0/16", "IPv4"))
	assert.False(t, sgRulesContainIngressTCPPortRangeFromCIDR(rules, 9345, 9345, "10.0.0.0/16", "IPv4"))
	assert.False(t, sgRulesContainIngressTCPPortRangeFromCIDR(rules, 6443, 6443, "192.168.0.0/16", "IPv4"))
	assert.False(t, sgRulesContainIngressTCPPortRangeFromCIDR(rules, 6443, 6443, "10.0.0.0/16", "IPv6"))
	assert.False(t, sgRulesContainIngressTCPPortRangeFromCIDR(rules, 6443, 6443, "", "IPv4"))

	egress := rules
	egress[0].Direction = "egress"
	assert.False(t, sgRulesContainIngressTCPPortRangeFromCIDR(egress, 6443, 6443, "10.0.0.0/16", "IPv4"))
}

func TestSGRulesContainIngressFromRemoteGroup(t *testing.T) {
	t.Parallel()

	rules := []resource.SecurityGroupRuleDTO{{
		Direction:       "ingress",
		Ethertype:       "IPv4",
		RemoteGroupID:   "sg-shared",
		SecurityGroupID: "sg-shared",
	}}

	assert.True(t, sgRulesContainIngressFromRemoteGroup(rules, "sg-shared", "sg-shared"))
	assert.False(t, sgRulesContainIngressFromRemoteGroup(rules, "sg-other", "sg-shared"))
	assert.False(t, sgRulesContainIngressFromRemoteGroup(rules, "", "sg-shared"))
}

func sgDetail(id, name string, rules ...resource.SecurityGroupRuleDTO) resource.GetSecurityGroupResponse {
	return resource.GetSecurityGroupResponse{
		SecurityGroup: resource.SecurityGroup{ID: id, Name: name, SecurityGroupRules: rules},
	}
}

func TestResolveBootstrapSG_ReusesVerifiedDBRow(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "security_group_master").
		Return([]model.Resource{{ResourceUUID: "sg-m"}}, nil)
	m.network.EXPECT().GetSecurityGroupByID(gomock.Any(), "tok", "sg-m").
		Return(sgDetail("sg-m", "demo-master-sg"), nil)
	// persistTypedSecurityGroupUUID re-reads the typed row; generic row already present
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "security_group_master").
		Return([]model.Resource{{ResourceUUID: "sg-m"}}, nil)
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "security_group").
		Return([]model.Resource{{ResourceUUID: "sg-m"}}, nil)

	id, err := svc.resolveClusterBootstrapSecurityGroup(context.Background(), "tok", "cid-1",
		"demo-master-sg", "demo-master-sg", "security_group_master")
	require.NoError(t, err)
	assert.Equal(t, "sg-m", id)
}

func TestResolveBootstrapSG_ReconcilesByNameWhenDBRowStale(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "security_group_master").
		Return([]model.Resource{{ResourceUUID: "sg-old"}}, nil)
	// stale id points to a group with the wrong name
	m.network.EXPECT().GetSecurityGroupByID(gomock.Any(), "tok", "sg-old").
		Return(sgDetail("sg-old", "unrelated-sg"), nil)
	m.network.EXPECT().ListSecurityGroupsByName(gomock.Any(), "tok", "demo-master-sg").
		Return(resource.ListSecurityGroupsResponse{SecurityGroups: []resource.SecurityGroup{{ID: "sg-new"}}}, nil)
	// typed row must be repointed to the reconciled id
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "security_group_master").
		Return([]model.Resource{{ResourceUUID: "sg-old"}}, nil)
	m.resources.EXPECT().SetResourceUUIDByClusterAndType(gomock.Any(), "cid-1", "security_group_master", "sg-new").
		Return(nil)
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "security_group").Return(nil, nil)
	m.resources.EXPECT().CreateResource(gomock.Any(), &model.Resource{
		ClusterUUID: "cid-1", ResourceType: "security_group", ResourceUUID: "sg-new",
	}).Return(nil)

	id, err := svc.resolveClusterBootstrapSecurityGroup(context.Background(), "tok", "cid-1",
		"demo-master-sg", "demo-master-sg", "security_group_master")
	require.NoError(t, err)
	assert.Equal(t, "sg-new", id)
}

func TestResolveBootstrapSG_CreatesWhenAbsent(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "security_group_master").
		Return(nil, nil)
	m.network.EXPECT().ListSecurityGroupsByName(gomock.Any(), "tok", "demo-master-sg").
		Return(resource.ListSecurityGroupsResponse{}, nil)
	m.network.EXPECT().CreateSecurityGroup(gomock.Any(), "tok", gomock.Any()).DoAndReturn(
		func(_ context.Context, _ string, req request.CreateSecurityGroupRequest) (resource.CreateSecurityGroupResponse, error) {
			assert.Equal(t, "demo-master-sg", req.SecurityGroup.Name)
			return resource.CreateSecurityGroupResponse{SecurityGroup: resource.SecurityGroup{ID: "sg-created"}}, nil
		})
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "security_group_master").Return(nil, nil)
	m.resources.EXPECT().CreateResource(gomock.Any(), &model.Resource{
		ClusterUUID: "cid-1", ResourceType: "security_group_master", ResourceUUID: "sg-created",
	}).Return(nil)
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "security_group").Return(nil, nil)
	m.resources.EXPECT().CreateResource(gomock.Any(), &model.Resource{
		ClusterUUID: "cid-1", ResourceType: "security_group", ResourceUUID: "sg-created",
	}).Return(nil)

	id, err := svc.resolveClusterBootstrapSecurityGroup(context.Background(), "tok", "cid-1",
		"demo-master-sg", "demo-master-sg", "security_group_master")
	require.NoError(t, err)
	assert.Equal(t, "sg-created", id)
}

func TestResolveBootstrapSG_CreateRaceFallsBackToList(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "security_group_master").
		Return(nil, nil)
	m.network.EXPECT().ListSecurityGroupsByName(gomock.Any(), "tok", "demo-master-sg").
		Return(resource.ListSecurityGroupsResponse{}, nil)
	m.network.EXPECT().CreateSecurityGroup(gomock.Any(), "tok", gomock.Any()).
		Return(resource.CreateSecurityGroupResponse{}, errors.New("409 conflict"))
	// another worker created it between list and create
	m.network.EXPECT().ListSecurityGroupsByName(gomock.Any(), "tok", "demo-master-sg").
		Return(resource.ListSecurityGroupsResponse{SecurityGroups: []resource.SecurityGroup{{ID: "sg-race"}}}, nil)
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "security_group_master").Return(nil, nil)
	m.resources.EXPECT().CreateResource(gomock.Any(), &model.Resource{
		ClusterUUID: "cid-1", ResourceType: "security_group_master", ResourceUUID: "sg-race",
	}).Return(nil)
	m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "security_group").Return(nil, nil)
	m.resources.EXPECT().CreateResource(gomock.Any(), &model.Resource{
		ClusterUUID: "cid-1", ResourceType: "security_group", ResourceUUID: "sg-race",
	}).Return(nil)

	id, err := svc.resolveClusterBootstrapSecurityGroup(context.Background(), "tok", "cid-1",
		"demo-master-sg", "demo-master-sg", "security_group_master")
	require.NoError(t, err)
	assert.Equal(t, "sg-race", id)
}

func TestEnsureBootstrapSGRules_RequiresAllowedCIDRs(t *testing.T) {
	svc, _ := newClusterServiceForTest(t)

	_, err := svc.ensureClusterBootstrapSecurityGroupRules(context.Background(), "tok",
		&request.CreateClusterRequest{}, "sg-m", "sg-s")
	require.ErrorContains(t, err, "allowed CIDRs are required")
}

// Full step with every rule already in place: nothing may be created, and the
// final Neutron verification must pass.
func TestStepEnsureSecurityGroups_AlreadySatisfied(t *testing.T) {
	svc, m := newClusterServiceForTest(t)

	req := &request.CreateClusterRequest{
		ClusterName:  "demo",
		SubnetIDs:    []string{"sub-1"},
		AllowedCIDRS: []string{"203.0.113.0/24"},
	}
	cluster := &model.Cluster{ClusterUUID: "cid-1", ClusterSharedSecurityGroup: "sg-s"}

	// resolve x3 via verified DB rows
	for _, tc := range []struct{ typed, id, name string }{
		{"security_group_master", "sg-m", "demo-master-sg"},
		{"security_group_worker", "sg-w", "demo-worker-sg"},
		{"security_group_shared", "sg-s", "demo-cluster-shared-sg"},
	} {
		m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", tc.typed).
			Return([]model.Resource{{ResourceUUID: tc.id}}, nil).Times(2)
		m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "security_group").
			Return([]model.Resource{{ResourceUUID: tc.id}}, nil)
	}

	masterRules := []resource.SecurityGroupRuleDTO{
		ingressTCPRule("sg-m", 6443, 6443, "203.0.113.0/24", "IPv4"),
		ingressTCPRule("sg-m", 6443, 6443, "10.10.0.0/24", "IPv4"),
		ingressTCPRule("sg-m", 9345, 9345, "10.10.0.0/24", "IPv4"),
	}
	sharedSelfRef := resource.SecurityGroupRuleDTO{
		Direction: "ingress", Ethertype: "IPv4", RemoteGroupID: "sg-s", SecurityGroupID: "sg-s",
	}
	sharedRules := []resource.SecurityGroupRuleDTO{
		sharedSelfRef,
		ingressTCPRule("sg-s", 30000, 32767, "10.10.0.0/24", "IPv4"),
	}

	m.network.EXPECT().GetSecurityGroupByID(gomock.Any(), "tok", "sg-m").
		Return(sgDetail("sg-m", "demo-master-sg", masterRules...), nil).AnyTimes()
	m.network.EXPECT().GetSecurityGroupByID(gomock.Any(), "tok", "sg-w").
		Return(sgDetail("sg-w", "demo-worker-sg"), nil).AnyTimes()
	m.network.EXPECT().GetSecurityGroupByID(gomock.Any(), "tok", "sg-s").
		Return(sgDetail("sg-s", "demo-cluster-shared-sg", sharedRules...), nil).AnyTimes()

	m.network.EXPECT().GetSubnetByID(gomock.Any(), "tok", "sub-1").
		Return(resource.SubnetResponse{Subnet: resource.SubnetWithDetails{CIDR: "10.10.0.0/24", IPVersion: 4}}, nil)

	// no CreateSecurityGroupRuleForIP / ForSG expectations: any create fails the test
	err := svc.stepEnsureSecurityGroupsAndRules(context.Background(), "tok", req, cluster)
	require.NoError(t, err)
}
