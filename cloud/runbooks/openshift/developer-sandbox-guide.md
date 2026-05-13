# Liferay DXP on the Red Hat Developer Sandbox

End-to-end walkthrough: free OpenShift project → Liferay welcome page in your
browser. The whole thing takes about 15 minutes plus boot time.

> **Audience.** Anyone who wants to validate the Liferay default Helm chart
> against OpenShift without standing up a real cluster. The Developer
> Sandbox is free, doesn't expire (you re-up it monthly), and runs on the
> same OpenShift bits as the paid product.

> **Order matters.** This guide creates the Route and the
> `liferay-network` ConfigMap *before* running `helm install`. The
> ConfigMap injects `company.default.virtual.host.name` so that when
> Liferay initializes its database on first boot, the default company is
> written with the right Route hostname from the start. If you install
> Liferay first and add the ConfigMap later, the DB is already populated
> with the old (default) virtual host and you'll fight the data forever.

## Prerequisites on your machine

- `oc` — the OpenShift CLI. Arch:
  ```
  yay -S openshift-client-bin
  ```
- `helm` ≥ 3.8 (needed for OCI registry support).
- A web browser for the sandbox console and Liferay UI.

## 1. Create the Developer Sandbox

1. Go to <https://developers.redhat.com/developer-sandbox> and sign in with a
   Red Hat account (create one for free if you don't have it).
2. Click **Start your sandbox**. The portal provisions an OpenShift project
   for you with a name like `your-username-dev`. Provisioning takes ~1 min.
3. When it lands, click **Launch your Developer Sandbox for Red Hat
   OpenShift** to open the web console.

What you get on the free tier:

| Resource | Limit |
|---|---|
| Memory | 7 GiB per project |
| CPU | 1 vCPU dedicated (burst higher) |
| Storage | 5 GiB persistent volumes |
| Sessions | 30 days (renewable) |

The values overlay in `examples/values-openshift.yaml` is sized to fit
inside this 7 GiB ceiling.

## 2. Log in from the CLI

1. In the web console, click your username (top-right) → **Copy login
   command**.
2. Click **Display Token**.
3. Copy the `oc login --token=… --server=…` line and paste it into your
   terminal:
   ```
   oc login --token=sha256~... --server=https://api.<sandbox>.openshiftapps.com:6443
   ```
4. Confirm you landed in the right project:
   ```
   oc project
   ```
   It should print `Using project "<your-username>-dev" ...`.

## 3. Create the Route first to claim the hostname

The OpenShift router assigns a hostname the moment the Route resource is
created — even before there's a backing Service to forward traffic to. We
use that to read the hostname now, write it into the ConfigMap in step 4,
and have Liferay come up with the right virtual host on first boot.

Apply the Route manifest:

```
oc apply -f /home/greg/repos/liferay/liferay-portal/cloud/runbooks/openshift/examples/route.yaml
```

See `examples/route.yaml` for inline reasoning. In short: edge TLS
termination using the cluster's wildcard cert, auto-generated hostname
(Developer Sandbox doesn't allow custom domains on the free tier), HTTP →
HTTPS redirect.

Read the assigned hostname into a shell variable — you'll use it twice:

```
ROUTE_HOST="$(oc get route liferay-default -o jsonpath='{.spec.host}')"
echo "$ROUTE_HOST"
```

You'll see something like
`liferay-default-your-username-dev.apps.rm3.7wse.p1.openshiftapps.com`.

> The Route will return 503 until step 5 finishes — that's expected, the
> backing Service doesn't exist yet.

## 4. Create the `liferay-network` ConfigMap with that hostname

The default chart treats `localhost` as the default company's virtual host.
Hitting Liferay through the Route hostname instead would log
`NoSuchVirtualHostException` on every request. The `liferay-network`
ConfigMap supplies two env vars that fix it:

- `company.default.virtual.host.name` → the Route hostname
- `company.default.virtual.host.sync.on.startup=true` → re-apply on every
  boot (without this, the hostname only takes effect when the default
  company is *first created*)

Edit `examples/liferay-network-cm.yaml` and replace the placeholder
hostname with the one in `$ROUTE_HOST`. Or apply with a one-liner:

```
sed "s|liferay-default-gregoryamerson-dev.apps.rm3.7wse.p1.openshiftapps.com|${ROUTE_HOST}|" \
    /home/greg/repos/liferay/liferay-portal/cloud/runbooks/openshift/examples/liferay-network-cm.yaml \
    | oc apply -f -
```

Verify it landed:

```
oc get configmap liferay-network -o yaml
```

> **Why this ordering matters.** Liferay's `CompanyLocalServiceImpl` writes
> the default company row to the DB during initial bundle activation,
> using whatever value of `company.default.virtual.host.name` is set at
> that moment. If the ConfigMap isn't in place before that first boot, the
> DB ends up with `localhost` and only a retroactive sync (or admin-UI
> edit) will fix it. Doing it now means the data is right from day one.

## 5. Install the Liferay default chart

The chart is published as an OCI artifact at:

```
oci://us-central1-docker.pkg.dev/liferay-artifact-registry/liferay-helm-chart/liferay-default
```

Install it with the OpenShift overlay:

```
helm install liferay \
    oci://us-central1-docker.pkg.dev/liferay-artifact-registry/liferay-helm-chart/liferay-default \
    --version 0.6.0 \
    -f /home/greg/repos/liferay/liferay-portal/cloud/runbooks/openshift/examples/values-openshift.yaml
```

> **Working on chart changes locally?** Point at the local path instead of
> the OCI URL so your in-progress edits are picked up:
> ```
> helm install liferay /home/greg/repos/liferay/liferay-portal/cloud/helm/default \
>     -f /home/greg/repos/liferay/liferay-portal/cloud/runbooks/openshift/examples/values-openshift.yaml
> ```

What the overlay does (`examples/values-openshift.yaml` has the full
reasoning in inline comments — read that file for the *why*):

1. Replaces pinned-UID security contexts so the `restricted-v2` SCC can
   inject `runAsUser` / `fsGroup`.
2. Forces `fsGroupChangePolicy: Always` so a mid-write crash can't corrupt
   the PVC across restarts.
3. References the `liferay-network` ConfigMap from step 4 via
   `customEnvFrom`, so the virtual-host env vars flow into the pod.
4. Bumps the PVC from 1 GiB → 10 GiB (1 GiB fills up before Liferay
   finishes booting).
5. Raises memory limits to 6 GiB to fit Liferay JVM + sidecar Elasticsearch
   without OOMKill.
6. Relaxes startup / readiness / liveness probes to tolerate slower
   cold-start on a constrained cluster.

### Updating values later

If you tweak the overlay, re-render with `helm upgrade`:

```
helm upgrade liferay \
    oci://us-central1-docker.pkg.dev/liferay-artifact-registry/liferay-helm-chart/liferay-default \
    --version 0.6.0 \
    -f /home/greg/repos/liferay/liferay-portal/cloud/runbooks/openshift/examples/values-openshift.yaml
```

### Watch the boot

```
oc get pod -w
```

`liferay-default-0` cycles through `Pending` → `Init:…/N` → `PodInitializing`
→ `Running 0/1` → `Running 1/1`. First boot takes 5–15 minutes on the
sandbox (sidecar Elasticsearch download + OSGi bundle activation + schema
init). The startup probe budget is 32 min — well past that.

Tail Liferay's own log to see boot progress in real time:

```
oc logs -f liferay-default-0
```

Look for the Liferay banner followed by `Server startup in [...] milliseconds`.

## 6. Verify by logging into Liferay

1. Open `https://$ROUTE_HOST/` in a browser (substitute the actual host).
   The first request will be slow — Liferay populates its virtual-host
   cache on cold hit.
2. The default welcome page should render. Click **Sign In** (top-right).
3. Default credentials for a fresh DXP install:
   - **Email:** `test@liferay.com`
   - **Password:** `test`
4. Liferay forces a password reset on first login. Set a new password, then
   accept the terms of use and answer the password reminder prompt.
5. You should land in the Liferay control panel as the default admin. Done.
6. Sanity check: there should be no `NoSuchVirtualHostException` in the pod
   logs (`oc logs liferay-default-0 | grep -i NoSuchVirtualHost`).

## Cleanup

Uninstall everything you applied, in reverse order:

```
helm uninstall liferay
oc delete configmap liferay-network
oc delete -f /home/greg/repos/liferay/liferay-portal/cloud/runbooks/openshift/examples/route.yaml
oc delete pvc liferay-persistent-volume-liferay-default-0
```

The PVC has to be deleted explicitly — Helm leaves it behind on purpose so
data survives an accidental `helm uninstall`. On the sandbox, leaving it
behind also eats into your 5 GiB storage quota.

## Troubleshooting

| Symptom | Most likely cause | Where to look |
|---|---|---|
| Pod stuck in `Init:0/N` for >2 min | Image pull from `liferay/dxp:latest` | `oc describe pod liferay-default-0` → `Events` |
| Pod hits `CrashLoopBackOff` early | OOMKill | `oc describe pod liferay-default-0` → look for `Reason: OOMKilled` and `Exit Code: 137`. Bump `resources.limits.memory` in the overlay, but keep it under 7 GiB total project quota. |
| Pod runs but `AccessDeniedException: /opt/liferay/osgi/war` in logs | Chart version predates the `osgi/war` PVC mount | Upgrade to chart ≥ 0.6.0, or install from the local chart path. |
| `NoSuchVirtualHostException` on every request | ConfigMap missing or has the wrong hostname | `oc get configmap liferay-network -o yaml` — confirm the value matches the Route host. If it's wrong, fix the ConfigMap, then `oc delete pod liferay-default-0` to force a re-read on restart. If Liferay was *already* booted with the wrong value, `company.default.virtual.host.sync.on.startup=true` will heal it on the next restart. |
| Route returns 503 | Liferay still booting (or no Service yet) | `oc logs liferay-default-0` and wait for `Server startup in [...]`. |
| Route returns 200 but blank page | Liferay still indexing/initializing modules after the welcome page | Wait another minute and refresh. |
