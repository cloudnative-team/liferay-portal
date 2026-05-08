---
taxonomy-category-names:
  - Cloud
  - DXP Self-Hosted Installation, Maintenance, and Administration
  - Liferay Self-Hosted
  - Liferay PaaS
uuid: TBD
---

# CNE Kubernetes Ready: Quickstart on k3d

This quickstart deploys Liferay DXP on a local [k3d](https://k3d.io/) cluster in about 15 minutes, using ephemeral PostgreSQL and Elasticsearch instances inside the cluster. It exists to give platform engineers a working reference deployment before they begin the full Bring-Your-Own-Infrastructure path described in the onboarding guide.

!!! warning
    This setup is for evaluation only. Both database and search use `emptyDir` volumes — every restart wipes their data. The Liferay license is the bundled trial. **Do not adapt this configuration for production.** Use [`values-production.yaml`](./values-production.yaml) and your team's managed services for production.

## What You'll Deploy

```text
   k3d cluster (single node, k3s under the hood)
   └── namespace: liferay-quickstart
       ├── postgres        Deployment (1 replica, emptyDir)
       │   └── Service     postgres:5432
       ├── elasticsearch   Deployment (1 replica, emptyDir, security off)
       │   └── Service     elasticsearch:9200
       └── liferay-default StatefulSet (1 replica, 5 GiB PVC)
           └── Service     liferay-default:8080
```

End state: Liferay reachable on `http://localhost:8080` via `kubectl port-forward`, signed in as the auto-generated admin user.

## Prerequisites

| Tool                                                         | Version  | Why                                              |
| :----------------------------------------------------------- | :------- | :----------------------------------------------- |
| [Docker](https://docs.docker.com/get-docker/) or [Podman](https://podman.io/) | recent | Container runtime for k3d                        |
| [k3d](https://k3d.io/stable/#installation)                   | >= 5.6   | Local Kubernetes cluster (k3s in Docker)         |
| [kubectl](https://kubernetes.io/docs/tasks/tools/)           | >= 1.28  | Cluster access                                   |
| [Helm](https://helm.sh/docs/intro/install/)                  | >= 3.8   | Install the Liferay chart                        |

Allocate at least **8 GiB of memory** and **4 CPU cores** to your container runtime. Liferay's startup is memory-heavy.

The Elasticsearch container requires `vm.max_map_count` to be at least `262144` on the kernel that hosts the container runtime. On most Docker Desktop and Podman installs this is already set. To verify:

```bash
sysctl vm.max_map_count
```

If the value is lower, raise it:

```bash
sudo sysctl -w vm.max_map_count=262144
```

## Step 1: Create the k3d Cluster

```bash
k3d cluster create liferay-quickstart \
    --servers 1 \
    --agents 0 \
    --port "8080:8080@loadbalancer"
kubectl create namespace liferay-quickstart
```

The `--port` flag exposes the cluster's built-in Traefik load balancer on host port `8080`, which we'll use later for browser access.

Verify:

```bash
kubectl cluster-info --context k3d-liferay-quickstart
```

!!! tip
    k3d ships with the Rancher [`local-path`](https://github.com/rancher/local-path-provisioner) StorageClass enabled by default, so the Liferay PVC works out of the box — no extra configuration needed.

## Step 2: Deploy In-Cluster Dependencies

Save the following as `quickstart-deps.yaml`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
    name: postgres
    namespace: liferay-quickstart
spec:
    replicas: 1
    selector:
        matchLabels:
            app: postgres
    template:
        metadata:
            labels:
                app: postgres
        spec:
            containers:
                -   name: postgres
                    image: postgres:16
                    env:
                        -   name: POSTGRES_USER
                            value: liferay
                        -   name: POSTGRES_PASSWORD
                            value: liferaypass
                        -   name: POSTGRES_DB
                            value: lportal
                        -   name: PGDATA
                            value: /var/lib/postgresql/data/pgdata
                    ports:
                        -   containerPort: 5432
                            name: tcp
                    volumeMounts:
                        -   name: data
                            mountPath: /var/lib/postgresql/data
            volumes:
                -   name: data
                    emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
    name: postgres
    namespace: liferay-quickstart
spec:
    selector:
        app: postgres
    ports:
        -   name: tcp
            port: 5432
            targetPort: 5432
---
apiVersion: apps/v1
kind: Deployment
metadata:
    name: elasticsearch
    namespace: liferay-quickstart
spec:
    replicas: 1
    selector:
        matchLabels:
            app: elasticsearch
    template:
        metadata:
            labels:
                app: elasticsearch
        spec:
            containers:
                -   name: elasticsearch
                    image: docker.elastic.co/elasticsearch/elasticsearch:8.13.4
                    env:
                        -   name: discovery.type
                            value: single-node
                        -   name: xpack.security.enabled
                            value: "false"
                        -   name: ES_JAVA_OPTS
                            value: "-Xms1g -Xmx1g"
                    ports:
                        -   containerPort: 9200
                            name: http
                    volumeMounts:
                        -   name: data
                            mountPath: /usr/share/elasticsearch/data
            volumes:
                -   name: data
                    emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
    name: elasticsearch
    namespace: liferay-quickstart
spec:
    selector:
        app: elasticsearch
    ports:
        -   name: http
            port: 9200
            targetPort: 9200
```

Apply it and wait for both pods to be ready:

```bash
kubectl apply -f quickstart-deps.yaml

kubectl --namespace liferay-quickstart \
    wait --for=condition=Available deployment/postgres deployment/elasticsearch \
    --timeout=5m
```

## Step 3: Create Dependency Secrets

The Liferay chart consumes credentials from named Secrets via `customEnvFrom`.

```bash
kubectl --namespace liferay-quickstart create secret generic liferay-database \
    --from-literal=DATABASE_HOST=postgres \
    --from-literal=DATABASE_PORT=5432 \
    --from-literal=DATABASE_NAME=lportal \
    --from-literal=LIFERAY_JDBC_PERIOD_DEFAULT_PERIOD_USERNAME=liferay \
    --from-literal=LIFERAY_JDBC_PERIOD_DEFAULT_PERIOD_PASSWORD=liferaypass

kubectl --namespace liferay-quickstart create secret generic liferay-search \
    --from-literal=ELASTICSEARCH_URL=http://elasticsearch:9200
```

No object-storage Secret is needed; this quickstart uses Liferay's default filesystem store on the per-pod PVC.

## Step 4: Install the Liferay Chart

Save the following as `quickstart-values.yaml`:

```yaml
image:
    repository: liferay/dxp
    tag: "<dxp-image-tag>"   # use any current tag, e.g. 2024.q4.10
    pullPolicy: IfNotPresent

replicaCount: 1

resources:
    requests:
        cpu: 1000m
        memory: 4Gi
    limits:
        cpu: 4000m
        memory: 8Gi

volumeClaimTemplates:
    -   metadata:
            name: liferay-persistent-volume
        spec:
            accessModes:
                -   ReadWriteOnce
            resources:
                requests:
                    storage: 5Gi

customEnvFrom:
    x-liferay-database:
        -   secretRef:
                name: liferay-database
    x-liferay-search:
        -   secretRef:
                name: liferay-search

customEnv:
    x-liferay-database:
        -   name: LIFERAY_JDBC_PERIOD_DEFAULT_PERIOD_DRIVER_UPPERCASEC_LASS_UPPERCASEN_AME
            value: org.postgresql.Driver
        -   name: LIFERAY_JDBC_PERIOD_DEFAULT_PERIOD_URL
            value: jdbc:postgresql://${env.DATABASE_HOST}:${env.DATABASE_PORT}/${env.DATABASE_NAME}?useUnicode=true&characterEncoding=UTF-8&useFastDateParsing=false

configmap:
    data:
        com.liferay.portal.search.elasticsearch8.configuration.ElasticsearchConfiguration.config: |
            authenticationEnabled=B"false"
            httpSSLEnabled=B"false"
            networkHostAddresses=["$[env:ELASTICSEARCH_URL]"]
            operationMode="REMOTE"
            productionModeEnabled=B"true"

customVolumeMounts:
    x-liferay-search:
        -   mountPath: /opt/liferay/osgi/configs/com.liferay.portal.search.elasticsearch8.configuration.ElasticsearchConfiguration.config
            name: liferay-configmap
            subPath: com.liferay.portal.search.elasticsearch8.configuration.ElasticsearchConfiguration.config
```

Install:

```bash
helm upgrade --install liferay \
    oci://us-central1-docker.pkg.dev/liferay-artifact-registry/liferay-helm-chart/liferay-default \
    --namespace liferay-quickstart \
    --values quickstart-values.yaml
```

## Step 5: Wait for Liferay to Start

First-boot is slow — Liferay applies database migrations, seeds data, and registers OSGi modules. The chart's startup probe allows up to 15 minutes; expect 5-8 minutes on a laptop.

Watch the rollout:

```bash
kubectl --namespace liferay-quickstart \
    rollout status statefulset/liferay-default --timeout=20m
```

Tail the log if you want to see progress:

```bash
kubectl --namespace liferay-quickstart \
    logs -f statefulset/liferay-default
```

Wait for the line:

```text
Server startup in [n] milliseconds
```

## Step 6: Access Liferay

```bash
kubectl --namespace liferay-quickstart \
    port-forward svc/liferay-default 8080:8080
```

Open <http://localhost:8080> in a browser. The default first-run admin account is `test@liferay.com`. Retrieve the auto-generated password:

```bash
kubectl --namespace liferay-quickstart \
    get secret liferay-default \
    -o jsonpath='{.data.LIFERAY_DEFAULT_PERIOD_ADMIN_PERIOD_PASSWORD}' | base64 -d
echo
```

## Step 7: Smoke-Test Checklist

Confirm the install works end-to-end before declaring success:

- [ ] Welcome page loads at <http://localhost:8080>
- [ ] You can sign in as `test@liferay.com` with the password above
- [ ] **Search**: navigate to *Control Panel → Configuration → Search → Connections* and confirm the `REMOTE` connection to Elasticsearch reports `Active`
- [ ] **Database**: navigate to *Control Panel → System Settings → Foundation → Database* and confirm the JDBC URL matches your secret
- [ ] **Documents and Media**: upload a small image to the *Home* site's Documents and Media library; confirm the file is retrievable after refresh
- [ ] **License**: navigate to *Control Panel → Configuration → License Manager* and confirm the trial license is active

## Tear Down

```bash
helm --namespace liferay-quickstart uninstall liferay
kubectl delete -f quickstart-deps.yaml
kubectl delete namespace liferay-quickstart
k3d cluster delete liferay-quickstart
```

## Next Steps

- Read [`architecture-and-sizing.md`](./architecture-and-sizing.md) for the production reference architecture and sizing tiers.
- Move from this quickstart to your own cluster using [`values-production.yaml`](./values-production.yaml) as a starting point.
- Replace each in-cluster dependency with its managed equivalent: PostgreSQL → managed RDBMS; Elasticsearch → managed search; filesystem store → S3 / GCS / Azure Blob.
- Wire your real ingress, TLS source, and observability stack as described in the [onboarding guide](./cne-k8s-ready.md).

## Troubleshooting

| Symptom                                                  | Likely cause                                                                                  |
| :------------------------------------------------------- | :-------------------------------------------------------------------------------------------- |
| `elasticsearch` pod stuck `CrashLoopBackOff` with `max virtual memory areas vm.max_map_count [65530] is too low` | Raise the kernel parameter on the container host: `sudo sysctl -w vm.max_map_count=262144`.   |
| Liferay pod `OOMKilled`                                  | Allocate more memory to your container runtime (8 GiB minimum) or lower `resources.limits.memory`. |
| `liferay-default-0` stuck in `Pending`                   | k3d's `local-path` StorageClass should bind the PVC automatically. If it doesn't, run `kubectl get sc` to confirm `local-path` is `(default)` and add `storageClassName: local-path` to `volumeClaimTemplates`. |
| Database connection refused                              | The `postgres` Service is not yet `Available`. Check `kubectl get pods -n liferay-quickstart`. |
| Welcome page returns 500 with search errors              | Elasticsearch did not start before Liferay tried to connect. Restart the Liferay pod: `kubectl rollout restart statefulset/liferay-default -n liferay-quickstart`. |
