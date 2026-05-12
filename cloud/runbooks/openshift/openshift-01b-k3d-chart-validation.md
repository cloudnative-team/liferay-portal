# Runbook 01b — Validate `liferay-default` on k3d (stand-in for OpenShift)

**Audience:** anyone who wants to smoke-test the chart's OpenShift-style overlay and arbitrary-UID compatibility quickly, without standing up real OpenShift.

**Scope:** Same as runbook 01 (chart admission + first-boot), but on a local k3d cluster. Trades fidelity (no SCC mutation, no Routes) for setup time (~5 min vs hours).

**What this proves and doesn't:**
- ✅ Chart renders correctly under PSS `restricted` namespace enforcement.
- ✅ Liferay's `liferay/dxp` image runs under **arbitrary, high UID** (simulating OpenShift's SCC by setting `runAsUser` manually).
- ❌ Does **not** validate OpenShift's SCC mutation itself (no SCCs on k3d) — use runbook 01/01c/01d for that.
- ❌ OpenShift `Route` is OpenShift-only; not testable on k3d.

---

## 1. Prerequisites

| Requirement | Notes |
|---|---|
| Docker | k3d runs each node as a container. |
| `k3d` v5+ | https://k3d.io |
| `kubectl` | k3d auto-writes a kubeconfig for it. |
| `helm` v3+ | |
| 8+ GiB free RAM | Liferay pod requests ~6 GiB by default. |

**Arch Linux:** `paru -S k3d-bin`.

---

## 2. Create the cluster

```sh
k3d cluster create lcd-51503 \
  --servers 1 \
  --agents 0 \
  --k3s-arg "--disable=traefik@server:0" \
  --wait
kubectl get nodes
```

---

## 3. Create the namespace with `restricted` enforcement

```sh
kubectl create namespace liferay-validate
kubectl label namespace liferay-validate \
  pod-security.kubernetes.io/enforce=restricted \
  pod-security.kubernetes.io/enforce-version=latest \
  pod-security.kubernetes.io/warn=restricted \
  pod-security.kubernetes.io/audit=restricted
```

PSS `restricted` is the closest k3d equivalent to OpenShift's `restricted-v2` SCC. Key difference: PSS only **enforces** the rules; OpenShift **mutates** the pod to inject UIDs. So pinned `runAsUser: 1000` is allowed under PSS restricted (just not pinned UID 0).

---

## 4. Values overlay

Two useful test shapes:

### Mode A — install with the same `values-openshift.yaml` you'd use on real OpenShift (expected to fail at the kubelet check)

This proves the chart renders correctly but surfaces the `liferay/dxp` image's `USER liferay` (non-numeric) limitation: kubelet refuses to start the container because `runAsNonRoot: true` is set but no numeric `runAsUser` exists (we deliberately drop it expecting an SCC to inject — which k3d doesn't have). Real cross-platform signal; on OpenShift it's a non-issue.

Copy the chart-shipped example:

```sh
cp /home/greg/repos/liferay/liferay-portal/cloud/helm/default/examples/values-openshift.yaml ./
```

### Mode B — explicit arbitrary UID + supplemental group 0 (simulates OpenShift's SCC injection)

The meaty test. Save as `values-arbitrary-uid.yaml`:

```yaml
podSecurityContext:
    fsGroup: 0
    runAsNonRoot: true
    runAsUser: 1000700001
    seccompProfile:
        type: RuntimeDefault
    supplementalGroups: [0]
securityContext:
    allowPrivilegeEscalation: false
    capabilities:
        drop:
            -   ALL
    runAsNonRoot: true
    runAsUser: 1000700001
    seccompProfile:
        type: RuntimeDefault
resources:
    requests:
        cpu: 500m
        memory: 2Gi
    limits:
        cpu: 2000m
        memory: 4Gi
```

Exercises the exact pod spec OpenShift's SCC would synthesize.

---

## 5. Install

**Mode A:**

```sh
helm install liferay /home/greg/repos/liferay/liferay-portal/cloud/helm/default \
  --namespace liferay-validate \
  --values ./values-openshift.yaml
```

Expected failure mode on `liferay-prepopulate-data` init container:
```
Error: container has runAsNonRoot and image has non-numeric user (liferay),
cannot verify user is non-root
```

**Mode B:**

```sh
helm install liferay /home/greg/repos/liferay/liferay-portal/cloud/helm/default \
  --namespace liferay-validate \
  --values ./values-arbitrary-uid.yaml
```

Watch:
```sh
kubectl -n liferay-validate get pods -w
```

---

## 6. Validate (Mode B)

```sh
# UID inside the container
kubectl -n liferay-validate exec liferay-default-0 -c liferay-default -- id
# Expected: uid=1000700001 ... groups=0(root)

# Admission accepted
kubectl -n liferay-validate get events --field-selector reason=FailedCreate

# Init containers completed
kubectl -n liferay-validate describe pod liferay-default-0 | sed -n '/Init Containers/,/^Containers/p'

# Liferay JVM started
kubectl -n liferay-validate logs liferay-default-0 -c liferay-default --tail=40
```

---

## 7. Cleanup

```sh
helm -n liferay-validate uninstall liferay
kubectl delete namespace liferay-validate
k3d cluster delete lcd-51503
```

---

## Troubleshooting

**Pod stuck `Pending` with no events:** Docker doesn't have enough memory for Liferay's 6 GiB request. Check `docker stats` and bump Docker's resource allocation.

**Why Mode A fails:** the `values-openshift.yaml` overlay deliberately omits `runAsUser` so OpenShift's SCC can inject it. On k3d there's no SCC, so the pod ends up with no `runAsUser` + `runAsNonRoot: true` + image `USER liferay` (non-numeric) — kubelet refuses to verify non-root status. Source: kubelet's `verifyRunAsNonRoot()` in `pkg/kubelet/kuberuntime/security_context_others.go`. Image-side fix is for `liferay/dxp` to use a numeric `USER 1000`; tracked separately.

**Mode B fails with permission denied writing to `/temp/liferay`:** the `liferay/dxp` image's `/opt/liferay` isn't group-0-writable. Drop `runAsUser` overlay and use the chart's default UID 1000 instead, or wait for the image fix.
