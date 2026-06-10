package ctxutil

import "context"

type clusterUUIDKey struct{}

// WithClusterUUID attaches a cluster UUID for log correlation (worker create/delete flows).
func WithClusterUUID(ctx context.Context, uuid string) context.Context {
	if uuid == "" {
		return ctx
	}
	return context.WithValue(ctx, clusterUUIDKey{}, uuid)
}

// ClusterUUID returns the cluster UUID from ctx, or empty if unset.
func ClusterUUID(ctx context.Context) string {
	s, _ := ctx.Value(clusterUUIDKey{}).(string)
	return s
}
