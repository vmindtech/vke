# VKE (vMind Kubernetes Engine)

vMind Kubernetes Engine. An OpenStack control plane that provisions and operates managed [RKE2](https://docs.rke2.io/) clusters, including load balancing, block storage, and node autoscaling.

[![License](https://img.shields.io/github/license/vmindtech/vke)](https://github.com/vmindtech/vke/blob/main/LICENSE)
[![Go](https://img.shields.io/github/go-mod/go-version/vmindtech/vke)](https://go.dev/)
[![CI](https://img.shields.io/github/actions/workflow/status/vmindtech/vke/test.yaml?label=tests)](https://github.com/vmindtech/vke/actions/workflows/test.yaml)

VKE is the API and worker you run next to an OpenStack cloud. A caller with a Keystone token asks for a cluster. VKE creates the OpenStack resources, bootstraps RKE2, and hands back a kubeconfig. Inside that cluster, Octavia, Cinder, and the node autoscaler are already wired, so application teams use ordinary Kubernetes objects.

## Contents

- [Features](#features)
- [How a cluster is built](#how-a-cluster-is-built)
- [Requirements](#requirements)
- [Quick start](#quick-start)
- [Configuration](#configuration)
- [Run](#run)
- [API](#api)
- [Development](#development)
- [Project layout](#project-layout)
- [Related projects](#related-projects)
- [Contributing](#contributing)
- [License](#license)

## Features

- **Managed RKE2 clusters** on OpenStack, with three control-plane nodes and one or more worker node groups.
- **Asynchronous create and delete.** The API records the request and returns immediately. A worker finishes the job and can resume it after a crash.
- **Kubernetes API on Octavia.** Each cluster gets an Octavia load balancer, health monitors, and either a public floating IP or a private endpoint. DNS is published through Cloudflare.
- **Node groups** with flavor, Cinder disk size, labels, taints, and a minimum and maximum size.
- **Built-in Octavia integration for `Service` `type: LoadBalancer`.** See below.
- **Built-in Cinder integration** for node disks and persistent volumes. See below.
- **Built-in node autoscaling.** A HorizontalPodAutoscaler is enough to grow and shrink the worker pool. See below.
- **Kubeconfig and error history** for each cluster, keyed by the OpenStack project.

### LoadBalancer services open an Octavia load balancer

VKE installs [cloud-provider-vke](https://github.com/vmindtech/cloud-provider-vke) into every cluster during bootstrap (`CLOUD_PROVIDER_VKE_VERSION`). That provider speaks to Octavia with the cluster application credential.

After the cluster is active, this Service is enough:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: web
spec:
  type: LoadBalancer
  selector:
    app: web
  ports:
    - port: 80
      targetPort: 8080
```

[cloud-provider-vke](https://github.com/vmindtech/cloud-provider-vke) creates the Octavia load balancer, listener, pool, and member set, and attaches a floating IP from `PUBLIC_NETWORK_ID`. Deleting the Service deletes that load balancer. No extra controller, Octavia CLI step, or cloud-config manifest is required from the cluster user.

This is separate from the load balancer VKE creates for the Kubernetes API itself. The API balancer is part of cluster provisioning. Service load balancers are created later, from inside the cluster, one per Service.

`LOADBALANCER_PROVIDER` selects the Octavia provider (for example `ovn`). The Keystone roles in `OPENSTACK_LOADBALANCER_ADMIN_ROLE` and `OPENSTACK_USER_OR_MEMBER_ROLE` are granted to the application credential so the provider can manage load balancers in the project.

### Cinder backs node disks and persistent volumes

Worker and control-plane root disks are Cinder volumes, created as Nova boot-from-volume devices (`BLOCK_STORAGE_ENDPOINT`). Worker size comes from `workerDiskSizeGB` on the create request. The same [cloud-provider-vke](https://github.com/vmindtech/cloud-provider-vke) release integrates the cluster with Cinder, so a PersistentVolumeClaim is fulfilled by a Cinder volume without installing a CSI driver yourself:

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: data
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 20Gi
```

### Node autoscaling follows the HPA

VKE installs [cluster-autoscaler](https://github.com/vmindtech/autoscaler-vke) (`CLUSTER_AUTOSCALER_VERSION`) and binds it to each worker node group’s minimum and maximum size.

You do not run a second scaler and you do not call the VKE API when traffic changes. Set a HorizontalPodAutoscaler:

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: web
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: web
  minReplicas: 2
  maxReplicas: 20
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
```

The HPA adds pods. When those pods cannot be scheduled, [cluster-autoscaler](https://github.com/vmindtech/autoscaler-vke) creates worker nodes on OpenStack, up to `workerNodeGroupMaxSize`. When the extra pods are gone, it removes nodes down to `workerNodeGroupMinSize`. The same bounds apply to node groups created later. Pending pods from any source, including an HPA, are what move the node count.

## How a cluster is built

```mermaid
flowchart LR
  Client[API client] -->|X-Auth-Token| API[cmd/api]
  API --> MySQL[(MySQL)]
  API --> Queue[RabbitMQ]
  Queue --> Worker[cmd/worker]
  Worker --> MySQL
  Worker --> OpenStack[Nova Neutron Octavia Keystone Cinder]
  Worker --> DNS[Cloudflare]
  Worker --> Nodes[RKE2 nodes]
  Nodes --> OpenStack
```

1. The API checks the Keystone token against the project, stores the cluster, creates an application credential, and publishes a `CLUSTER_CREATE` job. The HTTP response status is `CREATING`.
2. The worker walks a resumable state machine. Each step is idempotent.

   `INITIAL` → `LOADBALANCER` → `FLOATING_IP` → `SECURITY_GROUPS` → `SERVER_GROUPS` → `PORTS` → `COMPUTES` → `DNS` → `KUBECONFIG` → `COMPLETED`

3. The first control-plane node runs [vke-agent](https://github.com/vmindtech/vke-agent), which installs RKE2, [cloud-provider-vke](https://github.com/vmindtech/cloud-provider-vke), and [cluster-autoscaler](https://github.com/vmindtech/autoscaler-vke). Further control-plane and worker nodes join after the kubeconfig exists.
4. Delete follows the same pattern with a `CLUSTER_DELETE` job: load balancer, DNS, floating IP, nodes, security groups, then credentials.

The worker locks a job and refreshes that lock while the step runs. A dead worker’s lock expires after a few minutes and another worker retries the same step.

## Requirements

- Go 1.23 or newer, to build from source
- MySQL 8
- RabbitMQ
- An OpenStack project that can use Nova, Neutron, Octavia, Keystone, and Cinder
- A Glance image referenced by `IMAGE_REF`, and a Nova key pair the nodes can use
- A Cloudflare zone for the cluster API DNS name
- Network access from VKE to the OpenStack endpoints, and from the new nodes back to the VKE API (`ENDPOINT`)

Logstash is optional. With `LOGSTASH_HOST` empty, logs go to the process stdout as JSON.

## Quick start

```sh
git clone https://github.com/vmindtech/vke.git
cd vke
cp config.example.json config-development.json
```

Edit `config-development.json`. Files named `config-*.json` are gitignored. Point `MYSQL_URL` and `RABBITMQ_URL` at your local services, and fill the OpenStack, Cloudflare, and image values. `VKE_ENCRYPTION_KEY` encrypts application-credential secrets at rest. Generate it once and keep it stable for the life of the database.

Create the schema in an empty database. `scripts/vke.sql` is the current full schema and drops the VKE tables before recreating them, so run it only against a new database:

```sh
mysql -h 127.0.0.1 -u vke -p -e 'CREATE DATABASE IF NOT EXISTS vke CHARACTER SET utf8mb4;'
mysql -h 127.0.0.1 -u vke -p vke < scripts/vke.sql
```

Upgrading an older database that was created before `jobs`, `create_state`, or `cluster_subdomain_hash` existed:

```sh
make migrate-up DATABASE_URL='mysql://vke:password@tcp(127.0.0.1:3306)/vke?charset=utf8mb4&parseTime=true&loc=UTC'
```

Do not run `migrate-up` on a database just loaded from the current `scripts/vke.sql`. Those columns are already there, and the migration will stop on the duplicate column.

Start the API and the worker in two terminals:

```sh
export golang_env=development
go run ./cmd/api
```

```sh
export golang_env=development
go run ./cmd/worker
```

Check the process:

```sh
curl -s localhost/api/v1
curl -s localhost/readiness
```

`/api/v1` returns the app name, environment, and version. `/readiness` reports whether MySQL answers. `/liveness` reports whether the process is still serving.

Create a cluster with a Keystone token for `projectId`:

```sh
curl -s -X POST localhost/api/v1/cluster \
  -H 'Content-Type: application/json' \
  -H 'X-Auth-Token: <keystone-token>' \
  -H 'Idempotency-Key: create-web-1' \
  -d '{
    "clusterName": "web",
    "projectId": "<openstack-project-uuid>",
    "kubernetesVersion": "v1.31.4+rke2r1",
    "nodeKeyPairName": "vke",
    "clusterApiAccess": "public",
    "subnetIds": ["<subnet-uuid>"],
    "workerNodeGroupMinSize": 1,
    "workerNodeGroupMaxSize": 5,
    "workerInstanceFlavorUUID": "<flavor-uuid>",
    "masterInstanceFlavorUUID": "<flavor-uuid>",
    "workerDiskSizeGB": 50,
    "allowedCIDRs": ["10.0.0.0/8"]
  }'
```

The response carries `cluster_uuid` and `job_uuid` under `data`, with `cluster_status` set to `CREATING`. Poll `GET /api/v1/cluster/<cluster_uuid>` until `clusterStatus` is `Active`. While the worker runs, that field stays `Creating`. A failed step sets it to `Error`, and the message is available at `GET /api/v1/cluster/<cluster_uuid>/errors`. Then fetch the kubeconfig from `GET /api/v1/kubeconfig/<cluster_uuid>`. Repeating the create with the same `Idempotency-Key` returns the in-flight cluster instead of starting a second one.

`clusterApiAccess` is `public` or `private`. `public` allocates a floating IP and a DNS name. `private` keeps the API on the project network.

## Configuration

VKE reads `config-${golang_env}.json` from the working directory. Set `GO_VAULT_PATH` to load that file from another directory. The sample is [`config.example.json`](config.example.json).

| Key | Purpose |
| --- | --- |
| `APP_NAME`, `PORT`, `ENV`, `VERSION` | Process identity. `PORT` is the API listen port. `ENV=production` marks the production environment. |
| `ENDPOINT` | Public base URL of this API. Nodes call it during bootstrap. |
| `MYSQL_URL` | Go MySQL DSN. |
| `RABBITMQ_URL`, `RABBITMQ_QUEUE` | Job broker. The queue name defaults in deployments to `vke.jobs`. Both must be set or create and delete return HTTP 503. |
| `WORKER_CONCURRENCY` | Parallel jobs per worker process. Default 8, capped at 64. |
| `VKE_ENCRYPTION_KEY` | Secret used to encrypt application credentials in MySQL. Required. |
| `IDENTITY_ENDPOINT` | Keystone. Tokens are checked with `X-Auth-Token`. |
| `COMPUTE_ENDPOINT` | Nova. `NOVA_MICRO_VERSION` is sent on compute calls. |
| `NETWORK_ENDPOINT` | Neutron, for ports and security groups. |
| `LOAD_BALANCER_ENDPOINT`, `LOADBALANCER_PROVIDER` | Octavia, for the API balancer and for in-cluster Service load balancers. |
| `BLOCK_STORAGE_ENDPOINT` | Cinder, for boot volumes and persistent volumes. |
| `ENVOY_ENDPOINT` | Passed to the node agent as the cloud auth URL. |
| `PUBLIC_NETWORK_ID` | External network for API and Service floating IPs. |
| `IMAGE_REF` | Glance image used for every node. |
| `CLOUDFLARE_AUTH_TOKEN`, `CLOUDFLARE_ZONE_ID`, `CLOUDFLARE_DOMAIN` | DNS name of the cluster API. |
| `OPENSTACK_LOADBALANCER_ADMIN_ROLE`, `OPENSTACK_USER_OR_MEMBER_ROLE` | Roles placed on the per-cluster application credential. |
| `VKE_AGENT_VERSION`, `CLUSTER_AGENT_VERSION` | [vke-agent](https://github.com/vmindtech/vke-agent) and the cluster agent installed by the bootstrap script. |
| `CLOUD_PROVIDER_VKE_VERSION` | [cloud-provider-vke](https://github.com/vmindtech/cloud-provider-vke), the in-cluster Octavia and Cinder provider. |
| `CLUSTER_AUTOSCALER_VERSION` | [autoscaler-vke](https://github.com/vmindtech/autoscaler-vke), the in-cluster node autoscaler. |
| `LOGSTASH_HOST`, `LOGSTASH_PORT` | Optional UDP log shipping. Leave host empty or port `0` to log JSON to stdout. |

## Run

### Local

Development reload for the API, with [Air](https://github.com/air-verse/air):

```sh
export golang_env=development
air -c .air.toml
```

Air rebuilds `cmd/api` only. Keep a worker running with `go run ./cmd/worker`.

### Docker

The API image listens on port 80. The worker image has no published port. Both need the config file, MySQL, and RabbitMQ. The worker also needs `scripts/rke2-init-sh.tpl`, which the image already contains.

```sh
docker build -t vke-api -f Dockerfile .
docker build -t vke-worker -f Dockerfile.worker .

docker run --name vke-api \
  -e golang_env=production \
  -v "$PWD/config-production.json:/config-production.json:ro" \
  -p 8080:80 \
  vke-api

docker run --name vke-worker \
  -e golang_env=production \
  -v "$PWD/config-production.json:/config-production.json:ro" \
  vke-worker
```

Release images are published as `vmindtech/vke-application` on Docker Hub. There is no browser UI. Use the HTTP API.

## API

All cluster routes are under `/api/v1`. Send the OpenStack token in the `X-Auth-Token` header. Successful bodies use `{ "data": ... }`. Failures use `{ "error": { "code", "message", "cluster_uuid", "project_uuid" } }`.

| Method | Path | Action |
| --- | --- | --- |
| `GET` | `/api/v1` | Service metadata |
| `POST` | `/api/v1/cluster` | Create a cluster and enqueue `CLUSTER_CREATE` |
| `GET` | `/api/v1/cluster/:cluster_id` | Cluster status. `?details=true` adds load balancer, node groups, and subnets |
| `GET` | `/api/v1/cluster/project/:project_id` | Clusters in a project |
| `PUT` | `/api/v1/cluster/:cluster_id` | Update stored cluster fields |
| `DELETE` | `/api/v1/cluster/:cluster_id` | Enqueue `CLUSTER_DELETE` |
| `GET` | `/api/v1/kubeconfig/:cluster_id` | Download kubeconfig |
| `GET` | `/api/v1/cluster/:cluster_id/errors` | Provisioning and runtime errors |
| `GET` | `/api/v1/cluster/:cluster_id/flavors` | Flavors recorded for the cluster |
| `GET` | `/api/v1/cluster/:cluster_id/nodegroups` | List node groups |
| `POST` | `/api/v1/cluster/:cluster_id/nodegroups` | Add a worker node group |
| `GET` | `/api/v1/cluster/:cluster_id/nodegroups/:nodegroup_id` | One node group |
| `PUT` | `/api/v1/cluster/:cluster_id/nodegroups/:nodegroup_id` | Change min and max size |
| `DELETE` | `/api/v1/cluster/:cluster_id/nodegroups/:nodegroup_id` | Delete a node group |
| `GET` | `/api/v1/cluster/:cluster_id/nodegroups/:nodegroup_id/nodes` | Instances in the group |
| `PUT` | `/api/v1/cluster/:cluster_id/nodegroups/:nodegroup_id/nodes/add` | Add a node |
| `DELETE` | `/api/v1/cluster/:cluster_id/nodegroups/:nodegroup_id/nodes/:id` | Delete a node |
| `GET` | `/liveness` | Process is up |
| `GET` | `/readiness` | MySQL is reachable |

Create accepts camelCase or snake_case field names. `Idempotency-Key` makes retries safe.

## Development

```sh
make test-unit          # go test ./... -race
make lint               # golangci-lint run
make generate-mock-all  # regenerate mocks; CI fails if they drift
```

CI on pull requests runs `go build`, `go vet`, mock freshness, and the race-enabled test suite.

Node user-data is rendered from `scripts/rke2-init-sh.tpl`. Changing agent flags means changing that template and `GenerateUserDataFromTemplate` together.

## Project layout

```
cmd/api/            HTTP server
cmd/worker/         RabbitMQ consumer and job runner
internal/handler/   Request decoding and HTTP status mapping
internal/service/   Cluster state machine, OpenStack clients
internal/repository/ MySQL access
config/             JSON config loader
migrations/         Incremental upgrades for older databases
scripts/vke.sql     Current full schema for a new database
scripts/rke2-init-sh.tpl
pkg/                Logging, MySQL, RabbitMQ, health checks
```

## Related projects

These repositories are installed into every cluster by [vke-agent](https://github.com/vmindtech/vke-agent). Versions are pinned in the VKE config.

| Project | Role |
| --- | --- |
| [vke-agent](https://github.com/vmindtech/vke-agent) | Node bootstrap. Installs RKE2, then the cloud provider, Cinder storage, and the autoscaler. |
| [cloud-provider-vke](https://github.com/vmindtech/cloud-provider-vke) | OpenStack cloud provider. `Service` `type: LoadBalancer` becomes an Octavia load balancer. Persistent volumes are Cinder volumes. |
| [autoscaler-vke](https://github.com/vmindtech/autoscaler-vke) | Cluster autoscaler. Adds and removes worker nodes between the node group minimum and maximum when pods are pending. |

## Contributing

Open an issue or a pull request against `main`. Keep changes focused, and run `make test-unit` before pushing. New cluster behavior belongs in the worker state machine, with an idempotent step and a test next to the existing `internal/service/cluster_*_test.go` files.

Report security issues through [GitHub security advisories](https://github.com/vmindtech/vke/security/advisories/new) rather than a public issue. Do not commit `config-*.json`, tokens, or cloud endpoints.

## License

Apache License 2.0. See [LICENSE](LICENSE).
