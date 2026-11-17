package repository

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vmindtech/vke/internal/model"
)

func TestRepositoryInserts(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, table string
		call        func(*repositoryDB) error
	}{
		{"cluster", "clusters", func(d *repositoryDB) error {
			return NewClusterRepository(d).CreateCluster(ctx, &model.Cluster{ClusterUUID: "cid"})
		}},
		{"audit", "audit_log", func(d *repositoryDB) error {
			return NewAuditLogRepository(d).CreateAuditLog(ctx, &model.AuditLog{ClusterUUID: "cid", Event: "create"})
		}},
		{"kubeconfig", "kubeconfigs", func(d *repositoryDB) error {
			return NewKubeconfigRepository(d).CreateKubeconfig(ctx, &model.Kubeconfigs{ClusterUUID: "cid", KubeConfig: "encoded"})
		}},
		{"node group", "node_groups", func(d *repositoryDB) error {
			return NewNodeGroupsRepository(d).CreateNodeGroups(ctx, &model.NodeGroups{ClusterUUID: "cid", NodeGroupUUID: "ng"})
		}},
		{"resource", "resources", func(d *repositoryDB) error {
			return NewResourcesRepository(d).CreateResource(ctx, &model.Resource{ClusterUUID: "cid", ResourceType: "port", ResourceUUID: "port-1"})
		}},
		{"error", "errors", func(d *repositoryDB) error {
			return NewErrorRepository(d).CreateError(ctx, &model.Error{ClusterUUID: "cid", ErrorMessage: "failed"})
		}},
		{"job", "jobs", func(d *repositoryDB) error {
			return NewJobsRepository(d).CreateJob(ctx, &model.Job{ClusterUUID: "cid", JobUUID: "job", IdempotencyKey: "key"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, fail := range []bool{false, true} {
				boom := errors.New("insert failed")
				step := sqlStep{pattern: "^INSERT INTO `" + tc.table + "` \\(.*\\) VALUES \\(.*\\)$", affected: 1, check: func(args []driver.NamedValue) {
					values := []any{}
					for _, a := range args {
						values = append(values, a.Value)
					}
					assert.Contains(t, values, "cid")
				}}
				if fail {
					step.err = boom
				}
				db, _ := newRepositoryDB(t, step)
				err := tc.call(db)
				if fail {
					require.ErrorIs(t, err, boom)
				} else {
					require.NoError(t, err)
				}
			}
		})
	}
}

func TestRepositoryUpdates(t *testing.T) {
	ctx := context.Background()
	next := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, tc := range []struct {
		name, pattern string
		values        []any
		call          func(*repositoryDB) error
	}{
		{"cluster", "^UPDATE `clusters` SET .* WHERE `clusters`.`cluster_uuid` = \\?$", []any{"cid", "renamed"}, func(d *repositoryDB) error {
			cl := &model.Cluster{ClusterUUID: "cid", ClusterName: "renamed"}
			start := time.Now()
			err := NewClusterRepository(d).UpdateCluster(ctx, cl)
			assert.False(t, cl.ClusterUpdateDate.Before(start))
			return err
		}},
		{"delete cluster", "^UPDATE `clusters` SET .* WHERE `clusters`.`cluster_uuid` = \\?$", []any{"Deleted", "COMPLETED", "cid"}, func(d *repositoryDB) error {
			return NewClusterRepository(d).DeleteUpdateCluster(ctx, &model.Cluster{ClusterStatus: "Deleted", DeleteState: "COMPLETED", ClusterDeleteDate: next}, "cid")
		}},
		{"clear matching LB", "^UPDATE `clusters` SET `cluster_loadbalancer_uuid`=\\? WHERE cluster_uuid = \\? AND cluster_loadbalancer_uuid = \\?$", []any{"", "cid", "lb"}, func(d *repositoryDB) error {
			return NewClusterRepository(d).ClearLoadbalancerUUIDIfMatches(ctx, "cid", "lb")
		}},
		{"kubeconfig", "^UPDATE `kubeconfigs` SET .* WHERE `kubeconfigs`.`cluster_uuid` = \\?$", []any{"encoded", "cid"}, func(d *repositoryDB) error { return NewKubeconfigRepository(d).UpdateKubeconfig(ctx, "cid", "encoded") }},
		{"node group", "^UPDATE `node_groups` SET .* WHERE `node_groups`.`node_group_uuid` = \\?$", []any{"ng", "workers"}, func(d *repositoryDB) error {
			return NewNodeGroupsRepository(d).UpdateNodeGroups(ctx, &model.NodeGroups{NodeGroupUUID: "ng", NodeGroupName: "workers"})
		}},
		{"resource UUID", "^UPDATE `resources` SET `resource_uuid`=\\? WHERE cluster_uuid = \\? AND resource_type = \\?$", []any{"port-1", "cid", "port"}, func(d *repositoryDB) error {
			return NewResourcesRepository(d).SetResourceUUIDByClusterAndType(ctx, "cid", "port", "port-1")
		}},
		{"delete exact resource", "^DELETE FROM `resources` WHERE cluster_uuid = \\? AND resource_type = \\? AND resource_uuid = \\?$", []any{"cid", "port", "port-1"}, func(d *repositoryDB) error {
			return NewResourcesRepository(d).DeleteResourcesByClusterTypeAndUUID(ctx, "cid", "port", "port-1")
		}},
		{"job queued", "^UPDATE `jobs` SET .* WHERE `jobs`.`job_uuid` = \\?$", []any{next, "QUEUED", "job"}, func(d *repositoryDB) error { return NewJobsRepository(d).MarkJobQueued(ctx, "job", next) }},
		{"job succeeded clears error", "^UPDATE `jobs` SET `last_error`=\\?,`status`=\\?,`updated_at`=\\? WHERE `jobs`.`job_uuid` = \\?$", []any{"", "SUCCEEDED", "job"}, func(d *repositoryDB) error { return NewJobsRepository(d).MarkJobSucceeded(ctx, "job") }},
		{"job failed", "^UPDATE `jobs` SET .* WHERE `jobs`.`job_uuid` = \\?$", []any{int64(2), "retryable", next, "FAILED", "job"}, func(d *repositoryDB) error {
			return NewJobsRepository(d).MarkJobFailed(ctx, "job", "retryable", next, 2)
		}},
		{"heartbeat only owned running job", "^UPDATE `jobs` SET .* WHERE job_uuid = \\? AND locked_by = \\? AND status = \\?$", []any{"job", "worker-1", "RUNNING"}, func(d *repositoryDB) error { return NewJobsRepository(d).TouchJobLock(ctx, "job", "worker-1") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, fail := range []bool{false, true} {
				boom := errors.New("update failed")
				step := sqlStep{pattern: tc.pattern, affected: 1, check: func(args []driver.NamedValue) {
					values := []any{}
					for _, a := range args {
						values = append(values, a.Value)
					}
					for _, want := range tc.values {
						assert.Contains(t, values, want)
					}
				}}
				if fail {
					step.err = boom
				}
				db, _ := newRepositoryDB(t, step)
				err := tc.call(db)
				if fail {
					require.ErrorIs(t, err, boom)
				} else {
					require.NoError(t, err)
				}
			}
		})
	}
}

func TestClaimJobAndRetireIdempotencyKey(t *testing.T) {
	for _, tc := range []struct {
		name string
		rows int64
		err  error
	}{{"acquired", 1, nil}, {"held by another worker", 0, nil}, {"database failure", 0, errors.New("failed")}} {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			db, _ := newRepositoryDB(t, sqlStep{query: "UPDATE `jobs` SET `locked_at`=?,`locked_by`=?,`status`=?,`updated_at`=? WHERE job_uuid = ? AND (status <> ? OR locked_at IS NULL OR locked_at < ?)", affected: tc.rows, err: tc.err, check: func(args []driver.NamedValue) {
				require.Len(t, args, 7)
				assert.Equal(t, "worker", args[1].Value)
				assert.Equal(t, "RUNNING", args[2].Value)
				assert.Equal(t, "job", args[4].Value)
				assert.Equal(t, "RUNNING", args[5].Value)
				assert.WithinDuration(t, start, args[0].Value.(time.Time), time.Second)
				assert.WithinDuration(t, start.Add(-3*time.Minute), args[6].Value.(time.Time), time.Second)
			}})
			got, err := NewJobsRepository(db).TryMarkJobStarted(context.Background(), "job", "worker", 3*time.Minute)
			assert.Equal(t, tc.rows > 0, got)
			require.ErrorIs(t, err, tc.err)
		})
	}
	db, _ := newRepositoryDB(t, sqlStep{query: "UPDATE `jobs` SET `idempotency_key`=?,`updated_at`=? WHERE `jobs`.`job_uuid` = ?", affected: 1, check: func(args []driver.NamedValue) {
		require.Len(t, args, 3)
		assert.Regexp(t, `^retired:job:[0-9]+$`, args[0].Value)
		assert.Equal(t, "job", args[2].Value)
	}})
	require.NoError(t, NewJobsRepository(db).RetireIdempotencyKey(context.Background(), "job"))
}

func TestRepositoryCompositionAndTransactions(t *testing.T) {
	for _, tc := range []struct {
		name                string
		beginErr, commitErr error
	}{{"success", nil, nil}, {"begin error", errors.New("begin failed"), nil}, {"commit error", nil, errors.New("commit failed")}} {
		t.Run(tc.name, func(t *testing.T) {
			db, script := newRepositoryDB(t)
			script.beginErr = tc.beginErr
			script.commitErr = tc.commitErr
			cl, au, kc, ng, rs, er, jb := NewClusterRepository(db), NewAuditLogRepository(db), NewKubeconfigRepository(db), NewNodeGroupsRepository(db), NewResourcesRepository(db), NewErrorRepository(db), NewJobsRepository(db)
			r := NewRepository(db, cl, au, kc, ng, rs, er, jb)
			assert.Same(t, cl, r.Cluster())
			assert.Same(t, au, r.AuditLog())
			assert.Same(t, kc, r.Kubeconfig())
			assert.Same(t, ng, r.NodeGroups())
			assert.Same(t, rs, r.Resources())
			assert.Same(t, er, r.Error())
			assert.Same(t, jb, r.Jobs())
			tx, err := r.StartDBTransaction(context.Background())
			assert.True(t, script.began)
			if tc.beginErr != nil {
				require.ErrorIs(t, err, tc.beginErr)
				assert.Nil(t, tx)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, tx)
			require.ErrorIs(t, r.CommitDBTransaction(tx), tc.commitErr)
			assert.True(t, script.committed)
		})
	}
}
