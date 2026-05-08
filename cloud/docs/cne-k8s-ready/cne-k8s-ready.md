---
taxonomy-category-names:
  - Cloud
  - DXP Self-Hosted Installation, Maintenance, and Administration
  - Liferay Self-Hosted
  - Liferay PaaS
uuid: TBD
---

# Cloud Native Experience: Kubernetes Ready Guide

The Cloud Native Experience (CNE) Kubernetes Ready path is for teams that already operate their own Kubernetes platform and want to run Liferay DXP on it using the official `liferay-default` Helm chart. Unlike the [AWS Ready](../cne-cloud-provider-ready/cne-aws-ready.md) and GCP Ready paths, this path does **not** provision infrastructure, GitOps tooling, or managed services. You bring the cluster and Liferay's dependencies (Elasticsearch, Database, Cloud Storage). Liferay provides the official chart and guide shown here.

!!! info
    Use this path when the AWS Ready or GCP Ready toolkits are not a fit — for example, on-premises clusters, OpenShift, regulated environments where you cannot run Liferay's Terraform stacks, or organizations that already standardize on a different GitOps controller, ingress, and observability stack.

!!! tip "Looking for something specific?"
    - **Want to see Liferay running first?** Jump to the [Quickstart on k3d](./quickstart.md) — about 15 minutes from zero to a working DXP on your laptop with ephemeral PostgreSQL and Elasticsearch in-cluster.
    - **Need to size your cluster, decide replicas, or understand what Liferay supports?** Read [Architecture, Sizing, and Support Model](./architecture-and-sizing.md) before you write `values.yaml`. It covers the runtime topology, Dev/Small/Medium/Large sizing tiers, and the boundary between Liferay-supported components and your team's responsibilities.
    - **Ready to install on your own cluster?** Continue with this guide.

## Who This Guide Is For

This guide assumes your team already operates a CNCF-conformant Kubernetes cluster and is comfortable with Helm-based deployments. Specifically, you should already have:

| Capability                          | Examples                                                       |
| :---------------------------------- | :------------------------------------------------------------- |
| A Kubernetes cluster                 | EKS, GKE, AKS, OpenShift, Rancher, kubeadm, on-prem            |
| An ingress or gateway               | Envoy Gateway, NGINX, Istio, Contour, OpenShift Router         |
| Continuous delivery / GitOps        | Argo CD, Flux, GitHub Actions, GitLab CI, Cloud Build, Azure Pipelines |
| A managed relational database        | Amazon RDS, Cloud SQL, Azure Database, on-prem PostgreSQL/MySQL |
| A managed search backend            | Elasticsearch, OpenSearch (managed or self-hosted)             |
| Object storage for documents/media  | Amazon S3, Google Cloud Storage, Azure Blob, MinIO             |
| A secrets store                     | AWS Secrets Manager, Vault, Azure Key Vault, sealed-secrets, Kubernetes Secrets |
| Observability                       | Prometheus, Grafana, Loki, Datadog, OpenTelemetry collectors   |

If any of these are missing and you want Liferay to provision them for you, use the [AWS Ready](../cne-cloud-provider-ready/cne-aws-ready.md) or GCP Ready paths instead.

## What You Get

The `liferay-default` Helm chart deploys:

- A Liferay DXP `StatefulSet` with cluster-aware `portal-cloud.properties` and unicast/cluster-link configuration
- A `ServiceAccount`, `Service`, and `ConfigMap` wired to the Liferay container
- Optional `HorizontalPodAutoscaler` or KEDA `ScaledObject` for autoscaling
- Optional Gateway API `HTTPRoute` and `NetworkPolicy` resources
- Init containers that prepopulate Liferay's data directory, wait for dependent services, and (optionally) overlay client extensions and OSGi modules from object storage

Everything else — provisioning the database, search cluster, object storage bucket, ingress controller, certificate manager, observability stack, and CI/CD — is your responsibility.

## Prerequisites

### Local Tools

| Tool                                                   | Purpose                                  |
| :----------------------------------------------------- | :--------------------------------------- |
| [kubectl](https://kubernetes.io/docs/tasks/tools/)     | Communicate with the Kubernetes cluster  |
| [Helm](https://helm.sh/docs/intro/install/) >= 3.8.0   | Install and upgrade the Liferay chart    |

Helm 3.8+ is required because the Liferay chart is distributed as an OCI artifact.

### Cluster Requirements

- Kubernetes 1.25 or newer.
- A `StorageClass` that supports `ReadWriteOnce` persistent volumes. The chart provisions a per-pod `PersistentVolumeClaim` named `liferay-persistent-volume`.
- Sufficient capacity for the default Liferay request (`2000m` CPU, `6Gi` memory per replica). Adjust `resources` for your sizing.
- If you plan to expose Liferay through Gateway API, the [Gateway API CRDs](https://gateway-api.sigs.k8s.io/) and a controller (Envoy Gateway, Istio, Contour, etc.) must be installed.

### External Dependencies

Provision and capture the connection details for each of the following before installing the chart:

| Dependency        | Required values                                                              |
| :---------------- | :--------------------------------------------------------------------------- |
| Database (PostgreSQL or MySQL) | JDBC URL, username, password                                    |
| Search (Elasticsearch or OpenSearch)        | URL, username, password                          |
| Object storage   | Bucket name, region, credentials (or a workload-identity binding)            |
| TLS certificate   | Certificate and private key for the Liferay hostname                         |
| Liferay license   | `license.xml` activation key                                                 |

Confirm your database and search engine versions against the [Liferay Compatibility Matrix](https://web.liferay.com/services/support/compatibility-matrix).

## Install the Helm Chart

The Liferay default chart is published as an OCI artifact:

```
oci://us-central1-docker.pkg.dev/liferay-artifact-registry/liferay-helm-chart/liferay-default
```

1. Create a namespace for the deployment:

   ```bash
   kubectl create namespace liferay-system
   ```

1. Choose a chart version. The most recent stable releases are:

    | Version | Status |
    | ------- | ------ |
    | `0.5.0` | Latest stable supporting DXP 2025-2026 LTS|
    | `0.4.0` | Preview release (not for production) |


    To inspect the metadata for a specific version:

    ```bash
    helm show chart \
       oci://us-central1-docker.pkg.dev/liferay-artifact-registry/liferay-helm-chart/liferay-default \
       --version <chart-version>
    ```

1. Install the chart with a `values.yaml` file (how to add contents to this values.yaml file is described in the next sections):

   ```bash
   helm upgrade --install liferay \
      oci://us-central1-docker.pkg.dev/liferay-artifact-registry/liferay-helm-chart/liferay-default \
      --version <chart-version> \
      --namespace liferay \
      --values values.yaml
   ```

!!! tip
    Start with a minimal `values.yaml` that points the chart at your image, database, and search backend. Verify the pod starts and reaches the Liferay welcome page before layering on TLS, autoscaling, and overlays.

## Wire External Dependencies

Liferay reads its database, search, and object-storage configuration from environment variables and OSGi configuration files. The recommended pattern on the Kubernetes Ready path is to put each dependency's connection details into its own purpose-named Kubernetes Secret, then surface them in the Liferay container through the chart's `customEnvFrom` extension point.

!!! info
    The AWS Ready and GCP Ready paths emit a single `managed-service-details` Secret from the `LiferayInfrastructure` Crossplane composite resource ([gcp-infrastructure-provider](https://github.com/liferay/liferay-portal/tree/master/cloud/helm/gcp-infrastructure)). On the Kubernetes Ready path there is no `LiferayInfrastructure` XR, so do **not** reuse that Secret name. Create per-concern Secrets instead — they are easier to reason about, rotate independently, and bind to per-Secret RBAC or `ExternalSecret` policies.

### 1. Store Dependency Credentials in Kubernetes Secrets

Create one Secret per dependency. The keys become environment variables in the Liferay container.

```bash
kubectl --namespace liferay create secret generic liferay-database \
   --from-literal=DATABASE_HOST=db.internal.example.com \
   --from-literal=DATABASE_PORT=5432 \
   --from-literal=DATABASE_NAME=lportal \
   --from-literal=DATABASE_USERNAME=liferay \
   --from-literal=DATABASE_PASSWORD='********'

kubectl --namespace liferay create secret generic liferay-search \
   --from-literal=ELASTICSEARCH_URL=https://search.internal.example.com:9200 \
   --from-literal=ELASTICSEARCH_USERNAME=liferay \
   --from-literal=ELASTICSEARCH_PASSWORD='********'

kubectl --namespace liferay create secret generic liferay-object-storage \
   --from-literal=S3_BUCKET_NAME=acme-liferay-documents \
   --from-literal=S3_BUCKET_REGION=us-east-1 \
   --from-literal=S3_ACCESS_KEY_ID=AKIA... \
   --from-literal=S3_SECRET_ACCESS_KEY='********'
```

!!! tip
    `kubectl create secret` is shown here for clarity. In production, do **not** check raw credentials into Git or create them imperatively. Source these Secrets from your existing vault using one of:

    - [External Secrets Operator](https://external-secrets.io/) with `ExternalSecret` resources backed by AWS Secrets Manager, Vault, Azure Key Vault, GCP Secret Manager, etc.
    - [HashiCorp Vault Agent injector](https://developer.hashicorp.com/vault/docs/platform/k8s/injector) writing files or env vars at pod startup.
    - [Sealed Secrets](https://github.com/bitnami-labs/sealed-secrets) committed to your GitOps repository.

    The Secret *names* and *keys* shown above remain the contract the Liferay chart consumes — only the source of truth changes.

### 2. Reference the Secrets via `customEnvFrom`

The chart's `customEnvFrom` extension point appends `envFrom` entries to the Liferay container. Each `secretRef` exposes every key in that Secret as an environment variable.

Open a values.yaml file and add the following contents.

```yaml
customEnvFrom:
   x-liferay-database:
      -   secretRef:
             name: liferay-database
   x-liferay-search:
      -   secretRef:
             name: liferay-search
   x-liferay-object-storage:
      -   secretRef:
             name: liferay-object-storage
```

After this, `${env.DATABASE_HOST}`, `${env.ELASTICSEARCH_URL}`, `${env.S3_BUCKET_NAME}`, and the rest are available to the JDBC URL, OSGi configurations, and any other Liferay env-var-driven setting.

### 3. Configure the Database Driver

Set the JDBC driver and URL through `customEnv`. Liferay maps `LIFERAY_*` variables to portal properties using its standard naming convention. The username and password come straight from the `liferay-database` Secret loaded in step 2 — Liferay reads `LIFERAY_JDBC_PERIOD_DEFAULT_PERIOD_USERNAME` and `LIFERAY_JDBC_PERIOD_DEFAULT_PERIOD_PASSWORD` directly, so add them as keys in that Secret instead of plumbing them through `customEnv`:

```bash
kubectl --namespace liferay create secret generic liferay-database \
   --from-literal=DATABASE_HOST=db.internal.example.com \
   --from-literal=DATABASE_PORT=5432 \
   --from-literal=DATABASE_NAME=lportal \
   --from-literal=LIFERAY_JDBC_PERIOD_DEFAULT_PERIOD_USERNAME=liferay \
   --from-literal=LIFERAY_JDBC_PERIOD_DEFAULT_PERIOD_PASSWORD='********'
```

Then set the driver and URL in `values.yaml`:

```yaml
customEnv:
   x-liferay-database:
      -   name: LIFERAY_JDBC_PERIOD_DEFAULT_PERIOD_DRIVER_UPPERCASEC_LASS_UPPERCASEN_AME
          value: org.postgresql.Driver
      -   name: LIFERAY_JDBC_PERIOD_DEFAULT_PERIOD_URL
          value: jdbc:postgresql://${env.DATABASE_HOST}:${env.DATABASE_PORT}/${env.DATABASE_NAME}?useUnicode=true&characterEncoding=UTF-8&useFastDateParsing=false
```

For MySQL, swap the driver class to `com.mysql.cj.jdbc.Driver` and adjust the JDBC URL accordingly.

### 4. Configure the Search Backend

#### DXP / Elasticsearch Compatibility

The connector that ships in your DXP image dictates which Elasticsearch version you can run. Pick the Elasticsearch version from this matrix before sizing or provisioning the cluster — the OSGi configuration PID (`elasticsearch7` vs `elasticsearch8`) follows from this choice.

| DXP version            | Bundled connector | Supported Elasticsearch | OSGi PID         |
| ---------------------- | ----------------- | ----------------------- | ---------------- |
| DXP 2026.Q1 LTS+       | Elasticsearch 8   | **8.19+** only          | `elasticsearch8` |
| DXP 2024.Q1 – 2025.Q4  | Elasticsearch 7   | 7.17.x, 8.8.x – 8.17.x  | `elasticsearch7` |
| DXP 7.4 GA – Update 92 | Elasticsearch 7   | 7.17.x, 8.8.x – 8.17.x  | `elasticsearch7` |
| DXP 7.3                | Elasticsearch 7   | 7.17.x, 8.8.x – 8.15.x  | `elasticsearch7` |

!!! warning
    DXP 2026.Q1 LTS will **not** start against Elasticsearch 8.18 or earlier — the native Elasticsearch 8 connector requires **8.19+**. Elasticsearch 7.17 reached end-of-life on 2026-01-15, which is why 2026.Q1 dropped the ES 7 connector entirely.

For the authoritative, version-by-version list, consult the [Search Engine Compatibility Matrix](https://support.liferay.com/w/search-engine-compatibility-matrix) on the Liferay Customer Portal.

Both connectors read configuration from `/opt/liferay/osgi/configs`. Use the `configmap.data` extension point to drop the OSGi configuration file in, then mount it with `customVolumeMounts`. Liferay resolves the `$[env:...]` placeholders at runtime against the environment variables loaded from the `liferay-search` Secret.

#### Option A — Elasticsearch 7 Connector (DXP 7.3 through 2025.Q4)

```yaml
configmap:
   data:
      com.liferay.portal.search.elasticsearch7.configuration.ElasticsearchConfiguration.config: |
         authenticationEnabled=B"true"
         httpSSLEnabled=B"true"
         networkHostAddresses=["$[env:ELASTICSEARCH_URL]"]
         operationMode="REMOTE"
         password="$[env:ELASTICSEARCH_PASSWORD]"
         productionModeEnabled=B"true"
         username="$[env:ELASTICSEARCH_USERNAME]"

customVolumeMounts:
   x-liferay-search:
      -   mountPath: /opt/liferay/osgi/configs/com.liferay.portal.search.elasticsearch7.configuration.ElasticsearchConfiguration.config
          name: liferay-configmap
          subPath: com.liferay.portal.search.elasticsearch7.configuration.ElasticsearchConfiguration.config
```

#### Option B — Elasticsearch 8 Connector (DXP 2026.Q1 LTS+)

```yaml
configmap:
   data:
      com.liferay.portal.search.elasticsearch8.configuration.ElasticsearchConfiguration.config: |
         authenticationEnabled=B"true"
         httpSSLEnabled=B"true"
         networkHostAddresses=["$[env:ELASTICSEARCH_URL]"]
         password="$[env:ELASTICSEARCH_PASSWORD]"
         productionModeEnabled=B"true"
         username="$[env:ELASTICSEARCH_USERNAME]"

customVolumeMounts:
   x-liferay-search:
      -   mountPath: /opt/liferay/osgi/configs/com.liferay.portal.search.elasticsearch8.configuration.ElasticsearchConfiguration.config
          name: liferay-configmap
          subPath: com.liferay.portal.search.elasticsearch8.configuration.ElasticsearchConfiguration.config
```

!!! note
    The `operationMode` setting was deprecated in DXP 7.3 and is not present on the Elasticsearch 8 connector — `productionModeEnabled` covers the same intent. For OpenSearch, use the `opensearch` PID and consult the compatibility matrix for supported versions.

### 5. Configure Object Storage for Documents and Media

Liferay's Documents and Media store is configured through `LIFERAY_DL_PERIOD_STORE_PERIOD_IMPL` plus the OSGi configuration for the chosen store. The example below uses S3; substitute `GCSStore`, `AzureBlobStore`, or another store implementation as needed.

```yaml
customEnv:
   x-liferay-storage:
      -   name: LIFERAY_DL_PERIOD_STORE_PERIOD_IMPL
          value: com.liferay.portal.store.s3.S3Store

configmap:
   data:
      com.liferay.portal.store.s3.configuration.S3StoreConfiguration.config: |
         s3BucketName="$[env:S3_BUCKET_NAME]"
         s3Region="$[env:S3_BUCKET_REGION]"
         accessKey="$[env:S3_ACCESS_KEY_ID]"
         secretKey="$[env:S3_SECRET_ACCESS_KEY]"

customVolumeMounts:
   x-liferay-storage:
      -   mountPath: /opt/liferay/osgi/configs/com.liferay.portal.store.s3.configuration.S3StoreConfiguration.config
          name: liferay-configmap
          subPath: com.liferay.portal.store.s3.configuration.S3StoreConfiguration.config
```

How the pod authenticates to object storage depends on your platform. Two common options:

- **Static credentials** — keep `S3_ACCESS_KEY_ID` / `S3_SECRET_ACCESS_KEY` in the `liferay-object-storage` Secret as shown above.
- **Workload identity** (IRSA on EKS, Workload Identity on GKE, AAD Workload Identity on AKS) — bind the chart's `ServiceAccount` to a cloud IAM identity, drop the static credential keys from the Secret, and remove `accessKey` / `secretKey` from the OSGi config. Set `global.liferayServiceAccount.annotations` and `global.liferayServiceAccount.name` to wire the binding.

### 6. Mount the Liferay License

Store the activation key in a Secret and mount it into Liferay's auto-deploy directory.

```bash
kubectl --namespace liferay create secret generic liferay-license \
   --from-file=license.xml=./license.xml
```

```yaml
customEnv:
   x-license:
      -   name: LIFERAY_DISABLE_TRIAL_LICENSE
          value: "true"

customVolumes:
   x-license:
      -   name: liferay-license
          secret:
             secretName: liferay-license

customVolumeMounts:
   x-license:
      -   mountPath: /etc/liferay/mount/files/deploy/license.xml
          name: liferay-license
          subPath: license.xml
```

See [Activating Liferay DXP](../../setting-up-liferay/activating-liferay-dxp.md) for instructions on obtaining the `license.xml` file.

## Clustering and Autoscaling

For multi-replica deployments the chart automatically enables Tomcat session replication and Liferay cluster link, configured through `unicast.xml` and DNS-based membership against the headless Service.

```yaml
replicaCount: 3
```

To autoscale, enable HPA or KEDA:

```yaml
autoscaling:
   enabled: true
   type: hpa  # or "keda"
```

When using KEDA, set `autoscaling.keda.prometheusServerAddress` to your in-cluster Prometheus endpoint so KEDA can read the `hikari_active_connections` and `jvm_threads_current` metrics that the Liferay JMX exporter publishes.

## Adding Customizations (Client Extensions and OSGi modules) to DXP

TBD...

## Networking

The Liferay chart deliberately stops at the Kubernetes `Service` boundary — how external traffic reaches that Service is your platform's choice. This section covers the contract any ingress must honor and walks through the most common patterns: Gateway API (rendered by the chart), standard Kubernetes Ingress, cloud-provider load balancers, service mesh, OpenShift Routes, and edge-proxy / CDN setups such as Cloudflare.

### What the Chart Exposes

| Resource                               | Purpose                                                               | External traffic? |
| :------------------------------------- | :-------------------------------------------------------------------- | :---------------- |
| `Service liferay-default` (ClusterIP, port `8080`, named `http`) | The single entry point for HTTP requests to Liferay.   | Yes — this is what your ingress targets. |
| `Service liferay-default-headless` (port `7800`, named `cluster`) | Pod-to-pod cluster-link traffic for session replication and Liferay clustering. | **No** — must remain cluster-internal. |
| `HTTPRoute` (rendered when `network.enabled: true`) | Gateway API binding to an external `Gateway` you operate.        | Yes (via the Gateway). |

Any ingress, gateway, mesh sidecar, or load balancer should target the `liferay-default` Service on port `8080`. Never route external traffic to port `7800`; Liferay clustering depends on that channel being trusted, pod-to-pod only.

### Liferay-Specific Settings That Apply to Every Ingress

Regardless of which ingress technology you choose, configure Liferay so it trusts the `X-Forwarded-*` headers your proxy adds. Without this, Liferay will render absolute URLs (in emails, redirects, and OAuth callbacks) using the in-cluster scheme and port instead of the public ones.

```yaml
customEnv:
   x-liferay-forwarded:
      -   name: LIFERAY_WEB_PERIOD_SERVER_PERIOD_FORWARDED_PERIOD_PORT_PERIOD_ENABLED
          value: "true"
      -   name: LIFERAY_WEB_PERIOD_SERVER_PERIOD_FORWARDED_PERIOD_PROTOCOL_PERIOD_ENABLED
          value: "true"
      -   name: LIFERAY_WEB_PERIOD_SERVER_PERIOD_FORWARDED_PERIOD_HOST_PERIOD_ENABLED
          value: "true"
```

Plan for these cross-cutting concerns at every layer between the user and the pod:

| Concern                            | Why it matters                                                                                 |
| :--------------------------------- | :--------------------------------------------------------------------------------------------- |
| WebSocket / SSE upgrade            | Liferay collaboration, notifications, and Headless API live updates use long-lived connections. |
| HTTP request/response header size  | SAML/OIDC tokens and large cookies routinely exceed 8 KB. Bump proxy header buffers accordingly. |
| Idle / read timeouts               | Long-running operations (publishing, imports, exports) can exceed 60 seconds. Allow at least 5 minutes upstream. |
| Sticky sessions                    | Cluster session replication usually removes the need, but enabling cookie-based affinity reduces failover surprises. |
| HTTP/2 to upstream                 | Optional. Liferay (Tomcat) accepts HTTP/1.1 fine; HTTP/2 to upstream is a proxy preference, not a requirement. |
| Compression                        | Terminate gzip at the edge so Tomcat does not duplicate the work.                              |

### Pattern 1 — Gateway API (chart-rendered)

If your platform standardizes on Gateway API (Envoy Gateway, Istio with Gateway API, Contour, Cilium, Kong), let the chart render the `HTTPRoute` for you.

```yaml
network:
   enabled: true
   gatewayName: liferay-gateway
   endpointRef: http
   forceHttpsRedirect: true
   hostnames:
      - "liferay.example.com"
   timeouts:
      backendRequest: 300s
      request: 300s
```

The `Gateway` resource itself, its listeners, and its TLS configuration are out of scope for the chart — operate them as you would for any other workload.

### Pattern 2 — Standard Kubernetes Ingress (NGINX, Traefik, HAProxy)

If you have not adopted Gateway API yet, leave `network.enabled: false` and create your own `Ingress`. Example for the [ingress-nginx](https://kubernetes.github.io/ingress-nginx/) controller with [cert-manager](https://cert-manager.io/) issuing the certificate:

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
   name: liferay
   namespace: liferay
   annotations:
      cert-manager.io/cluster-issuer: letsencrypt-prod
      nginx.ingress.kubernetes.io/proxy-body-size: "100m"
      nginx.ingress.kubernetes.io/proxy-buffer-size: "16k"
      nginx.ingress.kubernetes.io/proxy-read-timeout: "300"
      nginx.ingress.kubernetes.io/proxy-send-timeout: "300"
      nginx.ingress.kubernetes.io/ssl-redirect: "true"
spec:
   ingressClassName: nginx
   tls:
      -   hosts:
             -   liferay.example.com
          secretName: liferay-example-tls
   rules:
      -   host: liferay.example.com
          http:
             paths:
                -   path: /
                    pathType: Prefix
                    backend:
                       service:
                          name: liferay-default
                          port:
                             name: http
```

Equivalent annotations exist for Traefik (`traefik.ingress.kubernetes.io/...`) and HAProxy Ingress (`haproxy.org/...`); consult your controller's documentation.

### Pattern 3 — Cloud-Provider Load Balancers

Most managed Kubernetes services offer a controller that provisions the cloud-native L7 load balancer directly from `Ingress` (or `Gateway`) resources. Use these when you want native integration with the cloud provider's certificate manager, WAF, and DDoS protections.

| Cloud   | Controller                                                                              | Typical pattern                                     |
| :------ | :-------------------------------------------------------------------------------------- | :-------------------------------------------------- |
| AWS     | [AWS Load Balancer Controller](https://kubernetes-sigs.github.io/aws-load-balancer-controller/) | `Ingress` annotated `alb.ingress.kubernetes.io/scheme: internet-facing`, ACM certificate ARN in `alb.ingress.kubernetes.io/certificate-arn`. |
| GCP     | [GKE Ingress for HTTP(S) Load Balancing](https://cloud.google.com/kubernetes-engine/docs/concepts/ingress) | `Ingress` with `kubernetes.io/ingress.class: gce`, plus `FrontendConfig` and `BackendConfig` for TLS, timeouts, and Cloud Armor. |
| Azure   | [Application Gateway for Containers](https://learn.microsoft.com/en-us/azure/application-gateway/for-containers/) or AGIC | Gateway API or `Ingress` annotated for AGIC; certificate stored in Azure Key Vault. |

In all three cases, terminate TLS at the cloud load balancer and let it forward HTTP (or re-encrypted HTTPS) to the `liferay-default` Service.

### Pattern 4 — Service Mesh (Istio, Linkerd, Cilium)

If Liferay runs inside a service mesh, expose it through the mesh's ingress gateway rather than a separate Ingress controller. With Istio:

```yaml
apiVersion: networking.istio.io/v1
kind: Gateway
metadata:
   name: liferay-gateway
   namespace: liferay
spec:
   selector:
      istio: ingressgateway
   servers:
      -   port:
             number: 443
             name: https
             protocol: HTTPS
          hosts:
             -   "liferay.example.com"
          tls:
             mode: SIMPLE
             credentialName: liferay-example-tls
---
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
   name: liferay
   namespace: liferay
spec:
   hosts:
      -   "liferay.example.com"
   gateways:
      -   liferay-gateway
   http:
      -   timeout: 300s
          route:
             -   destination:
                    host: liferay-default
                    port:
                       number: 8080
```

For Linkerd, mTLS is automatic between meshed pods; expose Liferay through your existing ingress (Pattern 2 or 3) with the Linkerd ingress mode enabled.

### Pattern 5 — OpenShift Route

OpenShift clusters can use the native `Route` resource instead of `Ingress`:

```yaml
apiVersion: route.openshift.io/v1
kind: Route
metadata:
   name: liferay
   namespace: liferay
spec:
   host: liferay.example.com
   to:
      kind: Service
      name: liferay-default
   port:
      targetPort: http
   tls:
      termination: edge
      insecureEdgeTerminationPolicy: Redirect
```

For end-to-end encryption to the pod, use `termination: reencrypt` and provide a destination CA certificate.

### Pattern 6 — Edge Proxy / CDN (Cloudflare, Akamai, Fastly, AWS CloudFront)

Many production deployments terminate TLS at a global edge layer rather than at the cluster. The cluster-side ingress in this case is just an internal target — often unprotected by a public certificate, sometimes only reachable through a private network or tunnel.

Two common topologies:

1. **CDN in front of a public ingress.** Cloudflare/Akamai/Fastly/CloudFront terminates TLS, applies the WAF and bot protection, and forwards requests over HTTPS to your existing Ingress (Pattern 2 or 3). The cluster ingress still presents a valid certificate to the CDN; lock its origin to the CDN's IP ranges so users cannot bypass the edge.

2. **CDN with a private tunnel into the cluster.** [Cloudflare Tunnel](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/), AWS PrivateLink + CloudFront, or an Azure Front Door Private Link Service connects directly to the `liferay-default` Service over a private path. The cluster does not need any public load balancer at all; only the tunnel daemon (for example, `cloudflared` running as a `Deployment` in the cluster) holds an outbound connection to the edge.

For Cloudflare Tunnel specifically, run `cloudflared` in the cluster and configure it to forward `liferay.example.com` to `http://liferay-default.liferay.svc.cluster.local:8080`. No `Ingress`, `Service` of type `LoadBalancer`, or public IP is required.

When using any edge proxy:

- Confirm the proxy preserves `Host`, `X-Forwarded-Proto`, and `X-Forwarded-For`. Liferay relies on these once `LIFERAY_WEB_PERIOD_SERVER_PERIOD_FORWARDED_PERIOD_*_PERIOD_ENABLED` is set (see [Liferay-Specific Settings](#liferay-specific-settings-that-apply-to-every-ingress) above).
- Disable the proxy's HTML/JS rewriting features. They will corrupt Liferay's resource URLs.
- If the proxy splits one TCP connection into many short-lived ones, increase Tomcat's `maxKeepAliveRequests` and the proxy's connection-pool size to avoid exhaustion under load.

### TLS Termination Strategy

The chart does not issue, store, or present certificates. Choose the layer that owns TLS based on what your platform already operates:

| Termination point          | When to use                                                                                                            |
| :------------------------- | :--------------------------------------------------------------------------------------------------------------------- |
| Edge (CDN / WAF)           | Default for internet-facing deployments. Centralizes certificate management; benefits from edge caching and DDoS protection. |
| Cloud load balancer        | Use when the cloud provider's certificate manager (ACM, Google-managed certificates, Azure Key Vault) is the system of record. |
| In-cluster gateway / ingress | Use when traffic is private (VPN, ExpressRoute, Direct Connect) or when cert-manager is already the source of truth.   |
| Pod (mesh sidecar)         | Use only when zero-trust policy mandates encryption end-to-end. Adds operational complexity to clustering and probes.  |

Whichever layer terminates TLS, ensure exactly **one** layer redirects HTTP to HTTPS. Multiple HTTPS-redirect layers (for example, a CDN forcing HTTPS *and* `forceHttpsRedirect: true` in the chart) produce redirect loops.

## Observability and Telemetry

The chart already ships the JMX → Prometheus exposition: a `jmx_prometheus_javaagent` is attached to the Liferay JVM and publishes metrics on a dedicated `metrics` port (the same port the autoscaling section's KEDA triggers read from). You do **not** need to attach an additional agent or sidecar to capture JMX — pointing your collector at the chart's existing endpoint is enough.

Useful metrics surfaced out of the box include `hikari_active_connections`, `hikari_idle_connections`, `jvm_threads_current`, `jvm_memory_bytes_used`, `tomcat_sessions_active_current`, and the standard `process_*` / `jvm_gc_*` families.

### Scrape the Built-In Endpoint

Whichever pipeline you use, the contract is the same: scrape `http://<pod-ip>:<metrics-port>/metrics` from each Liferay pod. The chart exposes the metrics port on its `Service` and adds the standard Prometheus pod annotations, so most platforms discover it automatically.

#### Pattern A — Prometheus Operator (`ServiceMonitor`)

If your platform runs `kube-prometheus-stack`, Rancher Monitoring, or any other operator-based Prometheus, drop in a `ServiceMonitor` that selects the Liferay Service:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
   name: liferay
   namespace: liferay
   labels:
      release: kube-prometheus-stack   # match your operator's release label
spec:
   selector:
      matchLabels:
         app.kubernetes.io/name: liferay
   endpoints:
      -   port: metrics
          interval: 30s
          path: /metrics
```

#### Pattern B — Plain Prometheus with Pod Annotations

A vanilla Prometheus install with `kubernetes_sd_configs` will pick up the chart's pods automatically — the chart sets `prometheus.io/scrape: "true"` and `prometheus.io/port` on the pod template. No extra configuration is required as long as your scrape config honors those annotations.

#### Pattern C — OpenTelemetry Collector → Any Backend

For Datadog, New Relic, Splunk Observability, Dynatrace, Elastic APM, Grafana Cloud, Honeycomb, Chronosphere, or any other vendor that accepts OTLP, run an OpenTelemetry Collector in the cluster with a Prometheus receiver pointed at the Liferay Service and the vendor-specific exporter on the back end:

```yaml
receivers:
   prometheus:
      config:
         scrape_configs:
            -   job_name: liferay
                kubernetes_sd_configs:
                   -   role: pod
                relabel_configs:
                   -   source_labels: [__meta_kubernetes_pod_label_app_kubernetes_io_name]
                       action: keep
                       regex: liferay

exporters:
   otlphttp/vendor:
      endpoint: https://<vendor-otlp-endpoint>
      headers:
         api-key: ${VENDOR_API_KEY}

service:
   pipelines:
      metrics:
         receivers:  [prometheus]
         exporters:  [otlphttp/vendor]
```

Swap the exporter for your platform: `datadog`, `awsxray` + `awsemf`, `signalfx`, `dynatrace`, `googlecloud`, etc. The receiver side stays the same regardless of destination.

#### Platform-Specific Shortcuts

| Platform                     | Recommended path                                                                                          |
| :--------------------------- | :-------------------------------------------------------------------------------------------------------- |
| Amazon EKS                   | ADOT Collector (a distribution of OTel) → CloudWatch / AMP / Datadog                                      |
| Google GKE                   | Google Cloud Managed Service for Prometheus — auto-scrapes `prometheus.io/*` annotations                  |
| Azure AKS                    | Azure Monitor managed Prometheus add-on, or OTel Collector → Application Insights                         |
| OpenShift                    | The platform's User Workload Monitoring stack — drop a `ServiceMonitor` in your namespace                 |
| Rancher / RKE                | Rancher Monitoring chart (Prometheus Operator under the hood) — same `ServiceMonitor` as Pattern A        |
| Datadog Agent (any cluster)  | Add the `ad.datadoghq.com/liferay.checks` annotation, or scrape via the agent's OpenMetrics integration   |
| Elastic Cloud / ECK          | Run Metricbeat with the `prometheus` module pointed at the Liferay Service, or OTel Collector → Elastic   |

!!! tip
    For logs and traces, the same OpenTelemetry Collector pattern applies — add a `filelog` receiver for container logs and an `otlp` receiver if you instrument any Liferay client extensions with the OTel Java agent. Keep one collector deployment per cluster and fan out by exporter so the Liferay chart stays vendor-neutral.

## GitOps Integration

This guide demonstrates a direct `helm upgrade --install` for clarity, but in production you should manage the release through whatever GitOps controller your team already uses. The two common patterns are:

- **Argo CD `Application`** pointing at a Git repository that contains your `values.yaml`, with the OCI chart referenced via `helm.repoURL`.
- **Flux `HelmRelease`** with a `HelmRepository` (or `OCIRepository`) source.

Both controllers can consume the OCI chart URL above. Liferay does not require a specific GitOps controller; the chart is Helm 3 standard.

## Verify the Deployment

1. Wait for the StatefulSet to become ready:

   ```bash
   kubectl --namespace liferay rollout status statefulset/liferay-default
   ```

1. Tail the Liferay logs and look for `Server startup in [n] milliseconds`:

   ```bash
   kubectl --namespace liferay logs -f statefulset/liferay-default
   ```

1. Port-forward and load the welcome page:

   ```bash
   kubectl --namespace liferay port-forward svc/liferay-default 8080:8080
   ```

   Then open `http://localhost:8080`.

The initial admin password is generated into the `liferay-default` Secret on first install:

```bash
kubectl --namespace liferay get secret liferay-default \
   -o jsonpath='{.data.LIFERAY_DEFAULT_PERIOD_ADMIN_PERIOD_PASSWORD}' | base64 -d
```

## Troubleshooting

| Symptom                                                  | Common Cause                                                                                              |
| :------------------------------------------------------- | :-------------------------------------------------------------------------------------------------------- |
| Pod stuck in `Init:0/n` on `liferay-wait-on-services`    | The chart's `dependencies` block lists an in-cluster service (such as a sidecar) that is not running.     |
| `Connection refused` to the database from Liferay        | `liferay-database` Secret missing, not referenced via `customEnvFrom`, or `LIFERAY_JDBC_*` URL not resolving the env-var placeholders. |
| Search not initializing                                  | Wrong Elasticsearch configuration PID for the running version, or `productionModeEnabled` set to `false`. |
| Documents and Media saving locally instead of object storage | `LIFERAY_DL_PERIOD_STORE_PERIOD_IMPL` not set, or store-specific OSGi config not mounted.            |
| 502 from the gateway                                     | Gateway listener targeting the wrong port; the Liferay Service exposes port `8080`, not `80`.             |
| License never activates                                  | License Secret mounted at the wrong path, or `LIFERAY_DISABLE_TRIAL_LICENSE` not set to `"true"`.         |


