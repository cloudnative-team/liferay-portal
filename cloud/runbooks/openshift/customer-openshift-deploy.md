# Deploying `liferay-default` on OpenShift

**Audience:** OpenShift devops engineers deploying Liferay Portal in your organization's existing OpenShift cluster.

**Scope:** how to install the `liferay-default` Helm chart on OpenShift with the correct security-context overrides for the SCC system, add an OpenShift `Route` for ingress, and validate the install. Out of scope: provisioning the OpenShift cluster itself, and wiring production-grade backing services (Postgres, Elasticsearch, S3-compatible object storage) — those are covered in a follow-up runbook.

If you are a Liferay maintainer validating chart changes locally, use one of the test runbooks instead:
- [`openshift-01-crc-and-helm-install.md`](./openshift-01-crc-and-helm-install.md) — native CRC
- [`openshift-01b-k3d-chart-validation.md`](./openshift-01b-k3d-chart-validation.md) — k3d substitute
- [`openshift-01c-developer-sandbox.md`](./openshift-01c-developer-sandbox.md) — Red Hat Developer Sandbox
- [`openshift-01d-fedora-vm-nested-crc.md`](./openshift-01d-fedora-vm-nested-crc.md) — Fedora VM running nested CRC

---

## 1. Prerequisites

| Requirement | Notes |
|---|---|
| OpenShift 4.x cluster | Customer-managed, ROSA, ARO, OpenShift Dedicated, or self-managed. |
| `oc` CLI installed and logged in | `oc whoami` should print your user. |
| `helm` v3+ on PATH | https://helm.sh/docs/intro/install/ |
| A target project (namespace) | Either an existing one or create one with `oc new-project <name>` — `new-project` adds the SCC UID-range annotations the chart needs. |
| The Liferay chart | Either via a published Helm repo (preview/release) or a local checkout of `cloud/helm/default`. |
| (If `liferay/dxp` is private) image pull secret | `oc -n <project> create secret docker-registry liferay-pull --docker-server=... --docker-username=... --docker-password=...` then add to values overlay (`pullSecrets: [{ name: liferay-pull }]`). |

Confirm your project has the SCC UID-range annotation OpenShift requires for arbitrary-UID workloads:

```sh
oc get namespace "$(oc project -q)" -o jsonpath='{.metadata.annotations.openshift\.io/sa\.scc\.uid-range}'; echo
```

You should see something like `1000940000/10000`. If empty, the namespace wasn't created via `oc new-project` — recreate it or annotate manually.

---

## 2. Why a values overlay is needed on OpenShift

The `liferay-default` chart is platform-neutral and ships with secure-by-default container/pod security contexts that pin `runAsUser: 1000` and `fsGroup: 1000`. OpenShift's `restricted-v2` SCC **rejects pods with pinned UIDs** — it injects a UID from the namespace's annotated range at admission time instead.

The chart accommodates this with a simple convention:

- `securityContext` and `podSecurityContext` in the chart values are **empty** (`{}`) by default.
- The chart renders `defaultSecurityContext` / `defaultPodSecurityContext` (which carry the pinned-UID values) as a **fallback** when the user-supplied slots are empty.
- When you supply a **non-empty** `securityContext` / `podSecurityContext` in your values overlay, the chart uses yours and ignores the defaults.

Your OpenShift overlay supplies non-empty security contexts that deliberately **omit** `runAsUser` and `fsGroup`, leaving the rest of the strict PSS profile in place.

---

## 3. Get the values overlay

The chart ships a ready-to-use overlay at [`cloud/helm/default/examples/values-openshift.yaml`](../helm/default/examples/values-openshift.yaml). Copy it next to your install command:

```sh
cp /path/to/liferay-portal/cloud/helm/default/examples/values-openshift.yaml ./
```

…or if you only have a packaged chart, grab the file directly from the chart's source repo at the same path.

The shipped overlay does exactly one thing: supply non-empty `podSecurityContext` / `securityContext` (which disables the chart's pinned-UID fallbacks) that deliberately **omit** `runAsUser` / `fsGroup`, so OpenShift's SCC injects them at admission time. Everything else (runAsNonRoot, drop ALL, seccomp RuntimeDefault, etc.) stays strict-PSS-clean.

Any further customization — resource sizing for your project quota, image pull secret, `nameOverride`, custom env — goes in the **same** file. Common additions:

```yaml
# Tune resources to your cluster's project quota.
resources:
    requests:
        cpu: 1000m
        memory: 4Gi
    limits:
        cpu: 4000m
        memory: 8Gi

# Image pull secret if liferay/dxp is private in your registry.
# pullSecrets:
#     -   name: liferay-pull

# Override the chart's name if you want a custom Service name.
# nameOverride: liferay-prod
```

Refer to [`cloud/helm/default/values.yaml`](../helm/default/values.yaml) for the full schema.

---

## 4. Install the chart

From a published Helm repo:

```sh
helm repo add liferay <REPO_URL>
helm repo update
helm install liferay liferay/liferay-default \
  --version <CHART_VERSION> \
  --namespace "$(oc project -q)" \
  --values ./values-openshift.yaml
```

From a local checkout:

```sh
helm install liferay /path/to/liferay-portal/cloud/helm/default \
  --namespace "$(oc project -q)" \
  --values ./values-openshift.yaml
```

Watch the pod:

```sh
oc get pods -w
```

First boot: image pull (~2 GiB compressed for `liferay/dxp:latest`) takes a few minutes, then init containers run, then Liferay starts. Total to `Running` is typically 5–10 minutes on a healthy cluster.

---

## 5. Add an OpenShift `Route`

The chart deliberately does **not** ship an OpenShift `Route` template — `Route` is OpenShift-only and the chart is platform-neutral. Add it as a separate resource. Two patterns are documented below; pick whichever matches your operational model.

### Pattern A — standalone manifest + `oc apply` (simplest)

The chart ships a ready-to-use Route manifest at [`cloud/helm/default/examples/route.yaml`](../helm/default/examples/route.yaml). Copy and apply:

```sh
cp /path/to/liferay-portal/cloud/helm/default/examples/route.yaml ./
# (Optional) edit the file to set an explicit `host:` if you don't want
# OpenShift to auto-generate <name>-<namespace>.<apps-domain>.
oc apply -n "$(oc project -q)" -f ./route.yaml
```

Best for: small deployments, manual changes, or environments where you don't already manage workloads via Helm-of-Helms.

### Pattern B — tiny wrapper chart (better for GitOps)

Create a local Helm chart that depends on `liferay-default` and adds the `Route` as a template. Layout:

```
my-liferay-openshift/
├── Chart.yaml
├── values.yaml
└── templates/
    └── route.yaml
```

**`Chart.yaml`:**

```yaml
apiVersion: v2
appVersion: latest
dependencies:
    -   name: liferay-default
        repository: <REPO_URL>          # or file://../path/to/cloud/helm/default
        version: <CHART_VERSION>
description: Liferay on OpenShift — wraps liferay-default with a Route and SCC-friendly defaults.
name: my-liferay-openshift
type: application
version: 0.0.1
```

**`values.yaml`:** the contents of `values-openshift.yaml` from step 3, nested under the `liferay-default:` namespace:

```yaml
liferay-default:
    podSecurityContext:
        runAsNonRoot: true
        seccompProfile:
            type: RuntimeDefault
    securityContext:
        allowPrivilegeEscalation: false
        capabilities:
            drop:
                -   ALL
        runAsNonRoot: true
        seccompProfile:
            type: RuntimeDefault
    resources:
        requests:
            cpu: 1000m
            memory: 4Gi
        limits:
            cpu: 4000m
            memory: 8Gi
```

**`templates/route.yaml`:**

```yaml
apiVersion: route.openshift.io/v1
kind: Route
metadata:
    labels:
        app.kubernetes.io/instance: {{ .Release.Name }}
        app.kubernetes.io/managed-by: {{ .Release.Service }}
        app.kubernetes.io/name: liferay-default
    name: {{ .Release.Name }}
    namespace: {{ .Release.Namespace }}
spec:
    port:
        targetPort: http
    to:
        kind: Service
        name: liferay-default
        weight: 100
    tls:
        termination: edge
        insecureEdgeTerminationPolicy: Redirect
    wildcardPolicy: None
```

Install:

```sh
( cd my-liferay-openshift && helm dependency update )
helm install liferay ./my-liferay-openshift --namespace "$(oc project -q)"
```

Best for: GitOps (Argo CD / Flux), multiple environments with the same shape, or any case where you want install/upgrade/rollback to cover Liferay + Route together.

---

## 6. Validate

```sh
# 6a. SCC injected a numeric UID
oc get pod liferay-default-0 -o jsonpath='{.spec.containers[0].securityContext.runAsUser}'; echo
oc get pod liferay-default-0 -o jsonpath='{.spec.securityContext}'; echo
# Expected: a numeric runAsUser inside the namespace UID range; fsGroup set too.

# 6b. SCC applied
oc get pod liferay-default-0 -o jsonpath='{.metadata.annotations.openshift\.io/scc}'; echo
# Expected: restricted-v2 (or restricted-v3 on newer clusters).

# 6c. Container running as the injected UID
oc exec liferay-default-0 -c liferay-default -- id
# Expected: uid=<injected> gid=0(root) groups=0(root)
# Group 0 is automatic — it's what makes /opt/liferay writable under arbitrary UIDs.

# 6d. Route serving traffic
ROUTE_HOST=$(oc get route liferay -o jsonpath='{.spec.host}')
echo "https://$ROUTE_HOST"
curl -sk -I "https://$ROUTE_HOST" | head -3
# Expected: any HTTP response (200, 302, or Liferay's startup page).

# 6e. Liferay JVM started
oc logs liferay-default-0 -c liferay-default --tail=50
# Expected: Tomcat startup, Liferay ASCII banner, OSGi bundles loading.
```

If any of these fail with `Forbidden` events, check `oc get events --sort-by=.lastTimestamp | tail -20` — SCC rejections show up there.

---

## 7. Day-2 operations

**Upgrade the chart:**
```sh
helm upgrade liferay liferay/liferay-default \
  --version <NEW_VERSION> \
  --namespace "$(oc project -q)" \
  --values ./values-openshift.yaml
```

**Rollback:**
```sh
helm rollback liferay --namespace "$(oc project -q)"
```

**Uninstall (leaves PVCs by default — delete those explicitly if you want a clean slate):**
```sh
helm -n "$(oc project -q)" uninstall liferay
oc delete pvc -l app=liferay-default
oc delete route liferay   # only if you used Pattern A
```

---

## 8. Next: production backing services

This runbook gets Liferay running with **embedded Hypersonic DB and an internal Elasticsearch sidecar** — not suitable for production. For production you'll wire in:

- A managed Postgres (or in-cluster via the CloudNativePG / Crunchy operator)
- A managed Elasticsearch / OpenSearch (or in-cluster via ECK / OpenSearch Operator)
- S3-compatible object storage (MinIO Tenant in-cluster, or your cloud provider's S3 / Azure Blob / GCS)
- A secrets-delivery mechanism (External Secrets Operator pulling from Vault / AWS Secrets Manager / Azure Key Vault)

These will be covered in a follow-up runbook (`openshift-02-backing-services.md`, planned).
