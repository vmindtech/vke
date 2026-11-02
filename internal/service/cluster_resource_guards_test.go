package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/vmindtech/vke/internal/dto/resource"
	"github.com/vmindtech/vke/internal/model"
)

func TestMasterPorts_ReadFailures(t *testing.T) {
	for _, operation := range []string{"sort", "register secondary"} {
		for _, scenario := range []string{"resource read", "port lookup", "member marker read"} {
			if operation == "sort" && scenario == "member marker read" {
				continue
			}
			t.Run(operation+"/"+scenario, func(t *testing.T) {
				svc, m := newClusterServiceForTest(t)
				boom := errors.New("dependency unavailable")
				if scenario == "resource read" {
					m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", "network_port_master").Return(nil, boom)
				} else {
					expectResourceRows(m, "network_port_master", "port-2")
					var lookupErr error
					if scenario == "port lookup" {
						lookupErr = boom
					}
					m.network.EXPECT().GetNetworkPort(gomock.Any(), "tok", "port-2").Return(resource.CreateNetworkPortResponse{Port: resource.Port{Name: "demo-master-2-port"}}, lookupErr)
					if scenario == "member marker read" {
						m.resources.EXPECT().GetResourceByClusterUUID(gomock.Any(), "cid-1", resLBMemberAPI).Return(nil, boom)
					}
				}
				if operation == "sort" {
					ports, err := svc.sortMasterPortResources(context.Background(), "tok", apiAccessCluster())
					require.ErrorIs(t, err, boom)
					assert.Nil(t, ports)
				} else {
					require.ErrorIs(t, svc.ensureSecondaryMasterLBPoolMembers(context.Background(), "tok", sgPortsRequest(), apiAccessCluster()), boom)
				}
			})
		}
	}
}

func TestOpenstackServerName_MatchesLiveMembersOnly(t *testing.T) {
	for _, scenario := range []string{"empty name", "missing group", "group error", "detail error", "deleted member", "name mismatch", "trimmed match"} {
		t.Run(scenario, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			boom := errors.New("nova unavailable")
			name := " demo-master-1 "
			if scenario == "empty name" {
				name = " "
			} else {
				var group resource.GetServerGroupResponse
				group.ServerGroup.Members = []string{" ", " vm-1 "}
				var groupErr error
				if scenario == "missing group" {
					groupErr = errors.New("404")
				}
				if scenario == "group error" {
					groupErr = boom
				}
				m.compute.EXPECT().GetServerGroup(gomock.Any(), "tok", "group-1").Return(group, groupErr)
				if groupErr == nil {
					var detailErr error
					if scenario == "detail error" {
						detailErr = boom
					}
					if scenario == "deleted member" {
						detailErr = errors.New("404")
					}
					serverName := "demo-master-2"
					if scenario == "trimmed match" {
						serverName = " demo-master-1 "
					}
					m.compute.EXPECT().GetInstancesDetail(gomock.Any(), "tok", "vm-1").Return(resource.OpenstacServersResponse{OpenstackServers: resource.OpenstackServer{Name: serverName}}, detailErr)
				}
			}
			found, err := svc.openstackServerNameInServerGroup(context.Background(), "tok", "group-1", name)
			if scenario == "group error" || scenario == "detail error" {
				require.ErrorIs(t, err, boom)
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, scenario == "trimmed match", found)
		})
	}
}

func TestLoadCreateRequest_RejectsMissingOrInvalidJSON(t *testing.T) {
	for _, payload := range []string{"", "{"} {
		t.Run("payload="+payload, func(t *testing.T) {
			svc, _ := newClusterServiceForTest(t)
			_, err := svc.loadCreateClusterRequest(context.Background(), &model.Cluster{ClusterUUID: "cid-1", CreateRequest: []byte(payload)})
			require.Error(t, err)
			if payload == "" {
				assert.ErrorContains(t, err, "missing create_request")
			}
		})
	}
}

func TestEnrichCreateRequest_NilRequest(t *testing.T) {
	svc, _ := newClusterServiceForTest(t)
	// No repository access is needed when there is no request to enrich.
	require.NotPanics(t, func() { svc.enrichCreateRequestFromNodeGroups(context.Background(), "cid-1", nil) })
}

func TestEnsureLBMembers_ReadinessFailureStopsNextMember(t *testing.T) {
	for _, scenario := range []string{"API", "register"} {
		t.Run(scenario, func(t *testing.T) {
			svc, m := newClusterServiceForTest(t)
			if scenario == "register" {
				expectResourceRows(m, resLBMemberAPI, "port-2")
			} else {
				expectResourceRows(m, resLBMemberAPI)
			}
			expectResourceRows(m, resLBMemberReg)
			expectResourceRows(m, resLBPoolAPI, "pool-api")
			expectResourceRows(m, resLBPoolReg, "pool-reg")
			m.network.EXPECT().GetNetworkPort(gomock.Any(), "tok", "port-2").Return(resource.CreateNetworkPortResponse{Port: resource.Port{FixedIps: []resource.FixedIp{{IpAddress: "10.0.0.12", SubnetID: "sub-1"}}}}, nil)
			boom := errors.New("LB not ready")
			m.lb.EXPECT().CheckLoadBalancerStatus(gomock.Any(), "tok", "lb-1").Return(resource.ListLoadBalancerResponse{}, boom)
			require.ErrorIs(t, svc.ensureLBPoolMembersForMasterPort(context.Background(), "tok", sgPortsRequest(), apiAccessCluster(), "port-2", 2), boom)
		})
	}
}
