# Runbook 01c — Validate `liferay-default` on Red Hat Developer Sandbox

**Audience:** anyone who wants to validate the chart against a **real OpenShift cluster** without standing up CRC locally. Fastest path to "is this actually OpenShift-clean": ~30 minutes from sign-up to a Liferay pod running under a real SCC.

**Scope:** Phase A only — chart admission, SCC behavior, Route ingress. The sandbox doesn't give you cluster-admin, so operators (CloudNativePG, ECK, MinIO, External Secrets) can't be installed.

**What this proves that k3d (runbook 01b) couldn't:**
- OpenShift's SCC actually mutates the pod spec to inject a UID (verifiable on the running pod).
- An OpenShift `Route` actually routes traffic — `curl` the host and reach Liferay.
- The chart deploys against a real OpenShift control plane, not a simulation.

**Sandbox limits:**
- ~7 GiB RAM, ~15 GiB ephemeral storage per project
- ~30 hours of uptime per 14-day window
- **No cluster-admin** — no operator installs, no CRD installs, no SCC modifications
- Two `*-dev` and `*-stage` namespaces auto-provisioned; you can't `oc new-project`

---

## 1. Sign up + get access

1. Visit https://developers.redhat.com/developer-sandbox. Sign in with your Red Hat developer account.
2. Click "Start using your sandbox". Provisioning takes ~5 min.
3. When ready: "Launch your Developer Sandbox" — opens the web console.
4. Top-right user menu → **Copy login command** → click "Display Token". Copy the full `oc login --token=… --server=…` line.
5. Paste into a terminal with `oc` on PATH (`paru -S openshift-client-bin` on Arch).
6. Verify:
   ```sh
   oc whoami
   oc projects
   oc project <your-username>-dev
   ```

`oc get scc` will fail with `Forbidden` — expected, you're not cluster-admin. SCCs still apply to your pods, you just can't enumerate them.

---

## 2. Inspect the namespace's UID range

```sh
PROJECT=$(oc project -q)
oc get namespace "$PROJECT" -o jsonpath='{.metadata.annotations.openshift\.io/sa\.scc\.uid-range}'; echo
oc get namespace "$PROJECT" -o jsonpath='{.metadata.annotations.openshift\.io/sa\.scc\.supplemental-groups}'; echo
```

You'll see something like `1000940000/10000` — the SCC will inject a UID starting there.

---

## 3. Add the preview chart repo

> Fill in the URL once the preview chart is published.

```sh
helm repo add liferay-preview <PREVIEW_REPO_URL>
helm repo update
helm search repo liferay-preview/liferay-default --versions
export LIFERAY_CHART_VERSION=<PREVIEW_CHART_VERSION>
```

Until the preview repo is available, install directly from a local checkout — see §5.

---

## 4. OpenShift values overlay

The `liferay-default` chart ships secure-by-default security contexts that pin `runAsUser: 1000` / `fsGroup: 1000` (via the `defaultSecurityContext` / `defaultPodSecurityContext` fallbacks). OpenShift's SCC rejects pinned UIDs — it injects them from the namespace UID range instead. You supply a non-empty `securityContext` / `podSecurityContext` that drops `runAsUser` / `fsGroup`; that disables the chart's fallbacks and lets the SCC do its job.

Copy the chart-shipped example and append Sandbox-specific resource tuning:

```sh
cp /home/greg/repos/liferay/liferay-portal/cloud/helm/default/examples/values-openshift.yaml ./
cat >> values-openshift.yaml <<'EOF'

# Sandbox has ~7 GiB per project; chart default asks 6 GiB.
# Cut limits to leave room for init containers.
resources:
    requests:
        cpu: 500m
        memory: 2Gi
    limits:
        cpu: 2000m
        memory: 4Gi
EOF
```

---

## 5. Install the chart

```sh
helm install liferay liferay-preview/liferay-default \
  --version "${LIFERAY_CHART_VERSION}" \
  --namespace "$(oc project -q)" \
  --values ./values-openshift.yaml
```

From a local checkout (no preview repo yet):

```sh
helm install liferay /home/greg/repos/liferay/liferay-portal/cloud/helm/default \
  --namespace "$(oc project -q)" \
  --values ./values-openshift.yaml
```

The chart doesn't ship an OpenShift `Route` — see §5a below.

### 5a. Add the OpenShift Route

**Approach 1 — standalone manifest + `oc apply` (simplest):**

```sh
oc -n "$(oc project -q)" apply \
  -f /home/greg/repos/liferay/liferay-portal/cloud/helm/default/examples/route.yaml
```

**Approach 2 — tiny wrapper chart:** create `my-liferay-openshift/` with `Chart.yaml` (depends on `liferay-default`), `values.yaml` (nest the overlay under `liferay-default:`), and `templates/route.yaml`. See [customer-openshift-deploy.md §5 Pattern B](./customer-openshift-deploy.md#pattern-b--tiny-wrapper-chart-better-for-gitops) for the full layout. Useful if you want install/upgrade/rollback to cover Liferay + Route together.

---

## 6. Validate — the OpenShift-only signals

**6a. SCC injected a UID inside the namespace range:**

```sh
oc get pod liferay-default-0 -o jsonpath='{.spec.containers[0].securityContext.runAsUser}'; echo
oc get pod liferay-default-0 -o jsonpath='{.spec.securityContext}'; echo
oc get pod liferay-default-0 -o jsonpath='{.metadata.annotations.openshift\.io/scc}'; echo
```

Expected: numeric `runAsUser` in the range from §2; SCC `restricted-v2` (or newer).

**6b. Container running as that UID:**

```sh
oc exec liferay-default-0 -c liferay-default -- id
```

Expected: `uid=1000940000(…) gid=0(root) groups=0(root)`.

**6c. Route serving traffic:**

```sh
ROUTE_HOST=$(oc get route liferay -o jsonpath='{.spec.host}')
echo "https://$ROUTE_HOST"
curl -sk -I "https://$ROUTE_HOST" | head -10
```

Any HTTP response means the router forwarded. Visit in a browser to see Liferay's first-run UI.

**6d. Pod healthy:**

```sh
oc logs liferay-default-0 -c liferay-default --tail=80
```

Expected: Tomcat startup, Liferay ASCII banner, OSGi bundles loading.

---

## 7. Sandbox limits

- `oc new-project` / `oc create namespace` — forbidden.
- Operator installs — no cluster-admin. CloudNativePG, ECK, MinIO, ESO all need it.
- Storage beyond per-project quota — sandbox PVCs are small.
- HPA via custom metrics — needs operators; basic resource HPA works.

---

## 8. Cleanup

```sh
helm -n "$(oc project -q)" uninstall liferay
oc delete route liferay
oc delete pvc -l app=liferay-default
```

Sandbox projects can't be deleted; reuse for the next chart change.

---

## 9. Next

This validates the chart admission + SCC + Route path on real OpenShift. Phase B (Postgres, ES, MinIO, ESO operators) needs cluster-admin — see [runbook 01d (Fedora VM + nested CRC)](./openshift-01d-fedora-vm-nested-crc.md) when ready.

For customer-facing deploys (production), see [`customer-openshift-deploy.md`](./customer-openshift-deploy.md).
