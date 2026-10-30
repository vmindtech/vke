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

func TestBootstrapSGRules_EachDependencyFailure(t *testing.T) {
	stages := []string{"subnet", "master read for allowed CIDR", "allowed CIDR rule", "master read for subnet", "subnet API rule", "master read for registration", "registration rule", "shared read", "self rule", "shared reload", "nodeport rule"}
	for _, failAt := range append([]string{"success"}, stages...) {
		t.Run(failAt, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			boom := errors.New("neutron unavailable")
			req := sgPortsRequest()
			req.AllowedCIDRS = []string{" ", "192.0.2.0/24"}
			var calls []any
			for _, stage := range stages {
				var err error
				if stage == failAt {
					err = boom
				}
				switch stage {
				case "subnet":
					calls = append(calls, m.network.EXPECT().GetSubnetByID(gomock.Any(), "tok", "sub-1").Return(resource.SubnetResponse{Subnet: resource.SubnetWithDetails{CIDR: "10.0.0.0/24", IPVersion: 4}}, err))
				case "master read for allowed CIDR", "master read for subnet", "master read for registration":
					calls = append(calls, m.network.EXPECT().GetSecurityGroupByID(gomock.Any(), "tok", "sg-m").Return(sgDetail("sg-m", "demo-master-sg"), err))
				case "shared read", "shared reload":
					calls = append(calls, m.network.EXPECT().GetSecurityGroupByID(gomock.Any(), "tok", "sg-s").Return(sgDetail("sg-s", "demo-cluster-shared-sg"), err))
				case "self rule":
					calls = append(calls, m.network.EXPECT().CreateSecurityGroupRuleForSG(gomock.Any(), "tok", request.CreateSecurityGroupRuleForSgRequest{SecurityGroupRule: request.SecurityGroupRuleForSG{Direction: "ingress", Ethertype: "IPv4", SecurityGroupID: "sg-s", RemoteGroupID: "sg-s"}}).Return(err))
				default:
					cidr, min, max, group := "10.0.0.0/24", "6443", "6443", "sg-m"
					if stage == "allowed CIDR rule" {
						cidr = "192.0.2.0/24"
					}
					if stage == "registration rule" {
						min, max = "9345", "9345"
					}
					if stage == "nodeport rule" {
						min, max, group = "30000", "32767", "sg-s"
					}
					calls = append(calls, m.network.EXPECT().CreateSecurityGroupRuleForIP(gomock.Any(), "tok", request.CreateSecurityGroupRuleForIpRequest{SecurityGroupRule: request.SecurityGroupRuleForIP{Direction: "ingress", Ethertype: "IPv4", Protocol: "tcp", SecurityGroupID: group, RemoteIPPrefix: cidr, PortRangeMin: min, PortRangeMax: max}}).Return(err))
				}
				if err != nil {
					break
				}
			}
			gomock.InOrder(calls...)
			got, err := svc.ensureClusterBootstrapSecurityGroupRules(context.Background(), "tok", req, "sg-m", "sg-s")
			if failAt == "success" {
				require.NoError(t, err)
				assert.Equal(t, []subnetRuleSource{{CIDR: "10.0.0.0/24", Ethertype: "IPv4"}}, got)
			} else {
				require.ErrorIs(t, err, boom)
				assert.Nil(t, got)
			}
		})
	}
}

func TestCollectSubnetRules_ValidationAndDeduplication(t *testing.T) {
	for _, scenario := range []string{"no IDs", "empty CIDR", "dual stack"} {
		t.Run(scenario, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			ids := []string{" "}
			if scenario == "empty CIDR" {
				ids = append(ids, "sub-1")
				m.network.EXPECT().GetSubnetByID(gomock.Any(), "tok", "sub-1").Return(resource.SubnetResponse{}, nil)
			}
			if scenario == "dual stack" {
				ids = append(ids, " sub-1 ", "sub-copy", "sub-v6")
				for _, id := range []string{"sub-1", "sub-copy"} {
					m.network.EXPECT().GetSubnetByID(gomock.Any(), "tok", id).Return(resource.SubnetResponse{Subnet: resource.SubnetWithDetails{CIDR: " 10.0.0.0/24 ", IPVersion: 4}}, nil)
				}
				m.network.EXPECT().GetSubnetByID(gomock.Any(), "tok", "sub-v6").Return(resource.SubnetResponse{Subnet: resource.SubnetWithDetails{CIDR: "2001:db8::/64", IPVersion: 6}}, nil)
			}
			got, err := svc.collectClusterSubnetRuleSources(context.Background(), "tok", ids)
			if scenario == "dual stack" {
				require.NoError(t, err)
				assert.Equal(t, []subnetRuleSource{{CIDR: "10.0.0.0/24", Ethertype: "IPv4"}, {CIDR: "2001:db8::/64", Ethertype: "IPv6"}}, got)
			} else {
				require.Error(t, err)
				assert.Nil(t, got)
			}
		})
	}
}

func TestRemoteGroupRule_RejectsWrongDirectionFamilyAndSource(t *testing.T) {
	for _, rule := range []resource.SecurityGroupRuleDTO{
		{Direction: "egress", Ethertype: "IPv4", RemoteGroupID: "sg-s", SecurityGroupID: "sg-s"},
		{Direction: "ingress", Ethertype: "IPv6", RemoteGroupID: "sg-s", SecurityGroupID: "sg-s"},
		{Direction: "ingress", Ethertype: "IPv4", RemoteGroupID: "other", SecurityGroupID: "sg-s"},
	} {
		assert.False(t, sgRulesContainIngressFromRemoteGroup([]resource.SecurityGroupRuleDTO{rule}, "sg-s", "sg-s"))
	}
}

func TestResolveBootstrapSG_PersistenceFailures(t *testing.T) {
	for _, source := range []string{"verified row", "name lookup", "create", "create race"} {
		for _, stage := range []string{"typed row", "generic row"} {
			t.Run(source+"/"+stage, func(t *testing.T) {
				svc, m := newClusterServiceForTest(t)
				boom := errors.New("database unavailable")
				if source == "verified row" {
					expectResourceRows(m, "security_group_master", "sg-m")
					m.network.EXPECT().GetSecurityGroupByID(gomock.Any(), "tok", "sg-m").Return(sgDetail("sg-m", "demo-master-sg"), nil)
				} else {
					expectResourceRows(m, "security_group_master")
					var listed resource.ListSecurityGroupsResponse
					if source == "name lookup" {
						listed.SecurityGroups = []resource.SecurityGroup{{ID: "sg-m"}}
					}
					m.network.EXPECT().ListSecurityGroupsByName(gomock.Any(), "tok", "demo-master-sg").Return(listed, nil)
					if source != "name lookup" {
						var createErr error
						if source == "create race" {
							createErr = errors.New("409")
						}
						m.network.EXPECT().CreateSecurityGroup(gomock.Any(), "tok", request.CreateSecurityGroupRequest{SecurityGroup: request.SecurityGroup{Name: "demo-master-sg", Description: "demo-master-sg"}}).Return(resource.CreateSecurityGroupResponse{SecurityGroup: resource.SecurityGroup{ID: "sg-m"}}, createErr)
						if createErr != nil {
							m.network.EXPECT().ListSecurityGroupsByName(gomock.Any(), "tok", "demo-master-sg").Return(resource.ListSecurityGroupsResponse{SecurityGroups: []resource.SecurityGroup{{ID: "sg-m"}}}, nil)
						}
					}
				}
				if stage == "typed row" {
					m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "security_group_master").Return(nil, boom)
				} else {
					expectResourceRows(m, "security_group_master", "sg-m")
					m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "security_group").Return(nil, boom)
				}
				id, err := svc.resolveClusterBootstrapSecurityGroup(context.Background(), "tok", "cid-1", "demo-master-sg", "demo-master-sg", "security_group_master")
				require.ErrorIs(t, err, boom)
				assert.Empty(t, id)
			})
		}
	}
}

func TestResolveBootstrapSG_RemoteFailures(t *testing.T) {
	for _, stage := range []string{"detail", "list", "listed empty ID", "created empty ID", "create fails", "race empty ID"} {
		t.Run(stage, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			boom := errors.New("neutron unavailable")
			if stage == "detail" {
				expectResourceRows(m, "security_group_master", "sg-m")
				m.network.EXPECT().GetSecurityGroupByID(gomock.Any(), "tok", "sg-m").Return(resource.GetSecurityGroupResponse{}, boom)
			} else {
				expectResourceRows(m, "security_group_master")
				var listed resource.ListSecurityGroupsResponse
				var listErr error
				if stage == "list" {
					listErr = boom
				}
				if stage == "listed empty ID" {
					listed.SecurityGroups = []resource.SecurityGroup{{ID: " "}}
				}
				m.network.EXPECT().ListSecurityGroupsByName(gomock.Any(), "tok", "demo-master-sg").Return(listed, listErr)
				if stage == "created empty ID" || stage == "create fails" || stage == "race empty ID" {
					var createErr error
					if stage != "created empty ID" {
						createErr = boom
					}
					m.network.EXPECT().CreateSecurityGroup(gomock.Any(), "tok", gomock.Any()).Return(resource.CreateSecurityGroupResponse{}, createErr)
					if createErr != nil {
						var raced resource.ListSecurityGroupsResponse
						if stage == "race empty ID" {
							raced.SecurityGroups = []resource.SecurityGroup{{ID: " "}}
						}
						m.network.EXPECT().ListSecurityGroupsByName(gomock.Any(), "tok", "demo-master-sg").Return(raced, nil)
					}
				}
			}
			id, err := svc.resolveClusterBootstrapSecurityGroup(context.Background(), "tok", "cid-1", "demo-master-sg", "demo-master-sg", "security_group_master")
			assert.Empty(t, id)
			if stage == "listed empty ID" || stage == "created empty ID" {
				require.ErrorContains(t, err, "empty id")
			} else {
				require.ErrorIs(t, err, boom)
			}
		})
	}
}

// All rules already exist; mutations would be unexpected in the final verification tests.
func bootstrapSGReady(m *clusterServiceMocks, failAt string, boom error) {
	master := []resource.SecurityGroupRuleDTO{ingressTCPRule("sg-m", 6443, 6443, "192.0.2.0/24", "IPv4"), ingressTCPRule("sg-m", 6443, 6443, "10.0.0.0/24", "IPv4"), ingressTCPRule("sg-m", 9345, 9345, "10.0.0.0/24", "IPv4")}
	shared := []resource.SecurityGroupRuleDTO{{Direction: "ingress", Ethertype: "IPv4", SecurityGroupID: "sg-s", RemoteGroupID: "sg-s"}, ingressTCPRule("sg-s", 30000, 32767, "10.0.0.0/24", "IPv4")}
	for _, tc := range []struct{ kind, id, name string }{{"master", "sg-m", "demo-master-sg"}, {"worker", "sg-w", "demo-worker-sg"}, {"shared", "sg-s", "demo-cluster-shared-sg"}} {
		expectResourceRows(m, "security_group_"+tc.kind, tc.id).AnyTimes()
		reads := 0
		m.network.EXPECT().GetSecurityGroupByID(gomock.Any(), "tok", tc.id).DoAndReturn(func(context.Context, string, string) (resource.GetSecurityGroupResponse, error) {
			reads++
			if (failAt == "worker resolve" && tc.kind == "worker") || (failAt == "shared resolve" && tc.kind == "shared") {
				return resource.GetSecurityGroupResponse{}, boom
			}
			rules := master
			if tc.kind == "shared" {
				rules = shared
			}
			final := (tc.kind == "master" && reads == 5) || (tc.kind == "shared" && reads == 3)
			if final {
				if failAt == tc.kind+" final read" {
					return resource.GetSecurityGroupResponse{}, boom
				}
				index := -1
				if tc.kind == "master" {
					index = map[string]int{"allowed rule": 1, "subnet API rule": 2, "registration rule": 3}[failAt] - 1
				} else {
					index = map[string]int{"self rule": 1, "nodeport rule": 2}[failAt] - 1
				}
				if index >= 0 {
					rules = append(append([]resource.SecurityGroupRuleDTO{}, rules[:index]...), rules[index+1:]...)
				}
			}
			return sgDetail(tc.id, tc.name, rules...), nil
		}).AnyTimes()
	}
	expectResourceRows(m, "security_group", "sg-m", "sg-w", "sg-s").AnyTimes()
	m.network.EXPECT().GetSubnetByID(gomock.Any(), "tok", "sub-1").Return(resource.SubnetResponse{Subnet: resource.SubnetWithDetails{CIDR: "10.0.0.0/24", IPVersion: 4}}, nil).AnyTimes()
}

func TestBootstrapSG_FinalVerificationAndPersistenceFailures(t *testing.T) {
	for _, stage := range []string{"worker resolve", "shared resolve", "save shared ID", "missing allowed CIDRs", "master final read", "shared final read", "allowed rule", "subnet API rule", "registration rule", "self rule", "nodeport rule", "success"} {
		t.Run(stage, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			boom := errors.New("dependency unavailable")
			bootstrapSGReady(m, stage, boom)
			cluster := &model.Cluster{ClusterUUID: "cid-1", ClusterSharedSecurityGroup: "sg-s"}
			req := sgPortsRequest()
			req.AllowedCIDRS = []string{" ", "192.0.2.0/24"}
			if stage == "save shared ID" {
				cluster.ClusterSharedSecurityGroup = ""
				m.clusters.EXPECT().UpdateCluster(gomock.Any(), &model.Cluster{ClusterUUID: "cid-1", ClusterSharedSecurityGroup: "sg-s"}).Return(boom)
			}
			if stage == "missing allowed CIDRs" {
				req.AllowedCIDRS = nil
			}
			err := svc.stepEnsureSecurityGroupsAndRules(context.Background(), "tok", req, cluster)
			switch stage {
			case "success":
				require.NoError(t, err)
			case "worker resolve", "shared resolve", "save shared ID", "master final read", "shared final read":
				require.ErrorIs(t, err, boom)
			case "missing allowed CIDRs":
				require.ErrorContains(t, err, "allowed CIDRs are required")
			default:
				require.ErrorContains(t, err, "still missing")
			}
		})
	}
}
