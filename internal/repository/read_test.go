package repository

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/vmindtech/vke/internal/model"
)

func TestRepositoryReads(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, query string
		args        []any
		columns     []string
		row         []driver.Value
		call        func(*repositoryDB) (any, error)
		want        any
	}{
		{"cluster", "SELECT * FROM `clusters` WHERE `clusters`.`cluster_uuid` = ? ORDER BY `clusters`.`id` LIMIT ?", []any{"cid", int64(1)}, []string{"cluster_uuid", "cluster_name"}, []driver.Value{"cid", "demo"}, func(d *repositoryDB) (any, error) { return NewClusterRepository(d).GetClusterByUUID(ctx, "cid") }, &model.Cluster{ClusterUUID: "cid", ClusterName: "demo"}},
		{"project clusters", "SELECT * FROM `clusters` WHERE `clusters`.`cluster_project_uuid` = ? AND `clusters`.`cluster_status` <> ?", []any{"project", "Deleted"}, []string{"cluster_uuid"}, []driver.Value{"cid"}, func(d *repositoryDB) (any, error) {
			return NewClusterRepository(d).GetClustersByProjectId(ctx, "project")
		}, []model.Cluster{{ClusterUUID: "cid"}}},
		{"kubeconfig", "SELECT * FROM `kubeconfigs` WHERE `kubeconfigs`.`cluster_uuid` = ? ORDER BY `kubeconfigs`.`id` LIMIT ?", []any{"cid", int64(1)}, []string{"cluster_uuid", "kubeconfig"}, []driver.Value{"cid", "encoded"}, func(d *repositoryDB) (any, error) { return NewKubeconfigRepository(d).GetKubeconfigByUUID(ctx, "cid") }, &model.Kubeconfigs{ClusterUUID: "cid", KubeConfig: "encoded"}},
		{"node group", "SELECT * FROM `node_groups` WHERE `node_groups`.`node_group_uuid` = ? ORDER BY `node_groups`.`id` LIMIT ?", []any{"ng", int64(1)}, []string{"cluster_uuid", "node_group_uuid"}, []driver.Value{"cid", "ng"}, func(d *repositoryDB) (any, error) { return NewNodeGroupsRepository(d).GetNodeGroupByUUID(ctx, "ng") }, &model.NodeGroups{ClusterUUID: "cid", NodeGroupUUID: "ng"}},
		{"node group parent", "SELECT * FROM `node_groups` WHERE `node_groups`.`node_group_uuid` = ? ORDER BY `node_groups`.`id` LIMIT ?", []any{"ng", int64(1)}, []string{"cluster_uuid"}, []driver.Value{"cid"}, func(d *repositoryDB) (any, error) {
			return NewNodeGroupsRepository(d).GetClusterProjectUUIDByNodeGroupUUID(ctx, "ng")
		}, "cid"},
		{"job UUID", "SELECT * FROM `jobs` WHERE `jobs`.`job_uuid` = ? ORDER BY `jobs`.`id` LIMIT ?", []any{"job", int64(1)}, []string{"job_uuid", "status"}, []driver.Value{"job", "QUEUED"}, func(d *repositoryDB) (any, error) { return NewJobsRepository(d).GetJobByUUID(ctx, "job") }, &model.Job{JobUUID: "job", Status: "QUEUED"}},
		{"job idempotency", "SELECT * FROM `jobs` WHERE `jobs`.`idempotency_key` = ? ORDER BY `jobs`.`id` LIMIT ?", []any{"key", int64(1)}, []string{"job_uuid", "status"}, []driver.Value{"job", "QUEUED"}, func(d *repositoryDB) (any, error) { return NewJobsRepository(d).GetJobByIdempotencyKey(ctx, "key") }, &model.Job{JobUUID: "job", Status: "QUEUED"}},
		{"errors newest first", "SELECT * FROM `errors` WHERE `errors`.`cluster_uuid` = ? ORDER BY created_at DESC", []any{"cid"}, []string{"cluster_uuid", "error_message"}, []driver.Value{"cid", "failed"}, func(d *repositoryDB) (any, error) { return NewErrorRepository(d).GetErrorsByClusterUUID(ctx, "cid") }, []model.Error{{ClusterUUID: "cid", ErrorMessage: "failed"}}},
		{"resources scoped by cluster and type", "SELECT * FROM `resources` WHERE cluster_uuid = ? AND resource_type = ?", []any{"cid", "port"}, []string{"cluster_uuid", "resource_uuid"}, []driver.Value{"cid", "port-1"}, func(d *repositoryDB) (any, error) {
			return NewResourcesRepository(d).GetResourceByClusterUUID(ctx, "cid", "port")
		}, []model.Resource{{ClusterUUID: "cid", ResourceUUID: "port-1"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, failed := range []bool{false, true} {
				t.Run(map[bool]string{false: "success", true: "database error"}[failed], func(t *testing.T) {
					boom := errors.New("database unavailable")
					step := sqlStep{query: tc.query, args: tc.args, columns: tc.columns, rows: [][]driver.Value{tc.row}}
					if failed {
						step.err = boom
					}
					db, _ := newRepositoryDB(t, step)
					got, err := tc.call(db)
					if failed {
						require.ErrorIs(t, err, boom)
						return
					}
					require.NoError(t, err)
					assert.Equal(t, tc.want, got)
				})
			}
		})
	}
}

func TestNodeGroupFiltersAndMissingRecord(t *testing.T) {
	for _, tc := range []struct {
		kind, status, query string
		args                []any
	}{
		{"", "", "SELECT * FROM `node_groups` WHERE `node_groups`.`cluster_uuid` = ?", []any{"cid"}},
		{"worker", "Active", "SELECT * FROM `node_groups` WHERE `node_groups`.`cluster_uuid` = ? AND `node_groups`.`node_groups_status` = ? AND `node_groups`.`node_groups_type` = ?", []any{"cid", "Active", "worker"}},
	} {
		t.Run(tc.kind+tc.status, func(t *testing.T) {
			for _, fail := range []bool{false, true} {
				step := sqlStep{query: tc.query, args: tc.args, columns: []string{"node_group_uuid"}, rows: [][]driver.Value{{"ng"}}}
				if fail {
					step.err = errors.New("failed")
				}
				db, _ := newRepositoryDB(t, step)
				got, err := NewNodeGroupsRepository(db).GetNodeGroupsByClusterUUID(context.Background(), "cid", tc.kind, tc.status)
				if fail {
					require.ErrorIs(t, err, step.err)
				} else {
					require.NoError(t, err)
					assert.Equal(t, []model.NodeGroups{{NodeGroupUUID: "ng"}}, got)
				}
			}
		})
	}
	q := "SELECT * FROM `node_groups` WHERE `node_groups`.`node_group_uuid` = ? ORDER BY `node_groups`.`id` LIMIT ?"
	db, _ := newRepositoryDB(t, sqlStep{query: q, args: []any{"missing", int64(1)}, columns: []string{"id"}})
	got, err := NewNodeGroupsRepository(db).GetNodeGroupByUUID(context.Background(), "missing")
	require.NoError(t, err)
	assert.Nil(t, got)
	db, _ = newRepositoryDB(t, sqlStep{query: "SELECT * FROM `clusters` WHERE `clusters`.`cluster_uuid` = ? ORDER BY `clusters`.`id` LIMIT ?", args: []any{"missing", int64(1)}, columns: []string{"id"}})
	_, err = NewClusterRepository(db).GetClusterByUUID(context.Background(), "missing")
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestClusterNameExists(t *testing.T) {
	for _, count := range []int64{0, 1} {
		db, _ := newRepositoryDB(t, sqlStep{query: "SELECT count(*) FROM `clusters` WHERE cluster_project_uuid = ? AND cluster_name = ? AND cluster_status <> ?", args: []any{"project", "demo", "Deleted"}, columns: []string{"count(*)"}, rows: [][]driver.Value{{count}}})
		got, err := NewClusterRepository(db).ClusterNameExists(context.Background(), "project", "demo")
		require.NoError(t, err)
		assert.Equal(t, count > 0, got)
	}
	boom := errors.New("failed")
	db, _ := newRepositoryDB(t, sqlStep{query: "SELECT count(*) FROM `clusters` WHERE cluster_project_uuid = ? AND cluster_name = ? AND cluster_status <> ?", err: boom})
	got, err := NewClusterRepository(db).ClusterNameExists(context.Background(), "project", "demo")
	require.ErrorIs(t, err, boom)
	assert.False(t, got)
}
