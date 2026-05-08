---
taxonomy-category-names:
  - Cloud
  - DXP Self-Hosted Installation, Maintenance, and Administration
  - Liferay Self-Hosted
  - Liferay PaaS
uuid: TBD
---

# CNE Kubernetes Ready: Architecture, Sizing, and Support Model

This page is the conceptual companion to the [Kubernetes ready guide](./cne-k8s-ready.md). Read it before you write `values.yaml`. It answers three questions every platform engineer asks first:

- What does the runtime topology actually look like?
- What size deployment do I need for my workload?
- What does Liferay support — and what is my team responsible for?

## Reference Architecture

Every Liferay Kubernetes Ready deployment has the same shape from the Liferay pod outward. Traffic flows top-to-bottom; the database, search, and object-storage tiers can sit in any of three placements.

```text
                          ┌──────────────────────────────────────┐
   Browser ──────────────►│ Edge: CDN / WAF        [optional]    │
                          │ Cloudflare · Akamai · Fastly · CF    │
                          └─────────────────┬────────────────────┘
                                            │ https
   ═════════════════════════════════════════╪═══════════════════════════════════
   Kubernetes cluster                       │
                                            ▼
                          ┌──────────────────────────────────────┐
                          │ Ingress / Gateway   (TLS terminates) │
                          │ NGINX · Envoy GW · Istio · ALB · GKE │
                          │ Ingress · OpenShift Route            │
                          └─────────────────┬────────────────────┘
                                            │ http
                                            ▼
                          ┌──────────────────────────────────────┐
                          │ Service: liferay-default (ClusterIP, │
                          │          port 8080, named "http")    │
                          └─────────────────┬────────────────────┘
                                            │  load-balanced
              ┌─────────────────────────────┼─────────────────────────────┐
              ▼                             ▼                             ▼
      ┌───────────────┐             ┌───────────────┐             ┌───────────────┐
      │ Liferay pod 1 │◄─── 7800 ──►│ Liferay pod 2 │◄─── 7800 ──►│ Liferay pod 3 │
      │ StatefulSet   │  cluster-   │               │  cluster-   │               │
      │               │   link via  │               │   link via  │               │
      │   ┌───────┐   │   headless  │   ┌───────┐   │   headless  │   ┌───────┐   │
      │   │  PVC  │   │   service   │   │  PVC  │   │   service   │   │  PVC  │   │
      │   └───────┘   │             │   └───────┘   │             │   └───────┘   │
      └───────┬───────┘             └───────┬───────┘             └───────┬───────┘
              │                             │                             │
              │   envFrom Secrets:                                        │
              │     liferay-database · liferay-search · liferay-object-storage
              │   volumeMount: liferay-license                            │
              ▼                             ▼                             ▼
                              ╔═══════════════════════════════╗
                              ║  Dependency endpoints         ║   ← any combination of
                              ║ (database · search · storage) ║      the three placements
                              ╚══════════════╤════════════════╝      below; mix per service
                                             │
            ┌────────────────────────────────┼────────────────────────────────┐
            ▼                                ▼                                ▼
   ┌──────────────────┐           ┌──────────────────┐           ┌──────────────────┐
   │ A. Chart-managed │           │ B. In-cluster,   │           │ C. External to   │
   │    (same release)│           │    customer-run  │           │    the cluster   │
   ├──────────────────┤           ├──────────────────┤           ├──────────────────┤
   │ rendered by the  │           │ Operator or sub- │           │ Managed service  │
   │ chart's          │           │ chart in the     │           │ behind a private │
   │ `dependencies:`  │           │ same or another  │           │ endpoint or peer-│
   │ block — extra    │           │ namespace:       │           │ ed VPC/VNet:     │
   │ StatefulSets in  │           │ CloudNativePG ·  │           │ RDS · Cloud SQL ·│
   │ the same release │           │ Zalando · ECK ·  │           │ Azure DB ·       │
   │ (e.g. quickstart │           │ Bitnami charts · │           │ OpenSearch Svc · │
   │ Postgres + ES).  │           │ MinIO Operator.  │           │ S3 · GCS.        │
   │                  │           │                  │           │                  │
   │ Eval / dev only. │           │ Self-managed     │           │ Default for      │
   │ Not a production │           │ data-plane;      │           │ production —     │
   │ pattern.         │           │ portable across  │           │ fully managed,   │
   │                  │           │ clouds and       │           │ multi-AZ HA, but │
   │                  │           │ on-prem.         │           │ vendor-locked.   │
   └──────────────────┘           └──────────────────┘           └──────────────────┘

   Secrets vault (AWS Secrets Manager · HashiCorp Vault · Azure Key Vault ·
   GCP Secret Manager) — synced into the Kubernetes Secrets that Liferay
   reads via External Secrets Operator, Vault Agent, or sealed-secrets.
   Same contract regardless of which placement (A/B/C) you pick.
```


## Sizing Tiers

These are starting points sized against typical Liferay DXP workloads. **Validate every tier with load testing against your content model and integration mix before promoting to production.** Sizing assumes Liferay 2024.Q4 or newer.

| Property                  | Dev / Sandbox      | Small              | Medium             | Large               |
| :------------------------ | :----------------- | :----------------- | :----------------- | :------------------ |
| Use case                  | Single dev or QA   | Light prod, staging | Standard prod     | Heavy prod, peak traffic |
| Concurrent users (target) | < 50               | < 500              | < 5,000            | 5,000+              |
| Liferay pod replicas      | 1                  | 2                  | 3                  | 5+ with HPA / KEDA  |
| Per-pod CPU (req → lim)   | 2000m → 4000m      | 2000m → 4000m      | 4000m → 8000m      | 8000m → 16000m      |
| Per-pod memory (req → lim) | 8 GiB → 12 GiB    | 12 GiB → 16 GiB    | 16 GiB → 24 GiB    | 24 GiB → 32 GiB     |
| JVM heap (`-Xmx`)         | 4 GiB              | 6 GiB              | 12 GiB             | 16 GiB              |
| PVC per pod               | 5 GiB              | 20 GiB             | 50 GiB             | 100 GiB+            |
| StorageClass              | default            | SSD-backed         | SSD-backed, multi-AZ | SSD-backed, multi-AZ, IOPS provisioned |
| Database                  | 2 vCPU / 4 GiB     | 4 vCPU / 16 GiB    | 8 vCPU / 32 GiB    | 16 vCPU / 64 GiB    |
| Database HA               | none               | single-AZ snapshot | multi-AZ replica   | multi-AZ replica + read replicas |
| Search nodes              | single node, 8 GiB | 3 nodes, 8 GiB ea. | 3 nodes, 16 GiB ea. | 5 nodes, 32 GiB ea. |
| Object storage            | filesystem (PVC)   | bucket             | bucket + lifecycle | bucket + lifecycle + replication |
| Backup cadence            | none               | daily              | hourly + daily off-cluster | continuous PITR + cross-region |
| Autoscaling               | off                | off                | HPA on CPU         | KEDA on Hikari and JVM threads |
| License                   | trial              | production         | production         | production          |


## Support Model

The Kubernetes Ready path is a **self-managed deployment model**. Liferay supports the `liferay-default` Helm chart and the Liferay DXP container image. Your team is responsible for the Kubernetes cluster, the ingress and TLS layer, the database and search backends, the object storage, the secrets store, the GitOps controller, the observability stack, and the operational policies (backup, disaster recovery, patching, capacity planning) around them.

### What is a "Supported Configuration"

For a deployment to qualify as a supported configuration on this path:

- The Liferay DXP image is a version listed on the [Liferay Compatibility Matrix](https://web.liferay.com/services/support/compatibility-matrix).
- The `liferay-default` chart version is one Liferay has published for that DXP version.
- The Kubernetes server version is within the upstream Kubernetes support window (currently the three most recent minor releases).
- The database and search engine versions are listed as supported on the matrix above.
- Resource requests and limits are not lower than the **Small** tier in this guide.
- The chart's pod security context and security context are unchanged or strictly more restrictive than the chart defaults.

Deployments that meet these criteria receive standard Liferay support response times. Deployments outside this scope are diagnosed on a best-effort basis and may be referred to your platform team for remediation.

### What to Include in a Support Case

When opening a support case, attach:

- Chart version (`helm list -n <namespace>`)
- Rendered manifests (`helm get manifest <release> -n <namespace>`)
- Effective values (`helm get values <release> -n <namespace>`)
- Liferay DXP image tag and digest
- Kubernetes server version (`kubectl version`)
- Pod logs from the affected `liferay-default-*` pods covering the incident window
- Output of `kubectl describe statefulset liferay-default -n <namespace>` and `kubectl describe pod <pod> -n <namespace>` for at least one affected pod
- Database and search engine vendor, version, and topology (single-node, replicated, multi-region)

### When Kubernetes Ready Path Is Not the Right Fit

If your team does not want to operate the surrounding infrastructure, use one of the managed Cloud Native Experience paths instead:

- [CNE AWS Ready](../cne-cloud-provider-ready/cne-aws-ready.md) — Liferay provisions and operates the EKS cluster, RDS database, OpenSearch domain, S3 bucket, Argo CD, and the GitOps repository structure.
- [CNE GCP Ready](../cne-cloud-provider-ready/cne-gcp-ready.me) — equivalent automation for Google Cloud (GKE, Cloud SQL, Elastic Cloud or self-managed search, GCS).
