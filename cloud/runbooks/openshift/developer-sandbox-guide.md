# Liferay DXP on the Red Hat Developer Sandbox

End-to-end walkthrough: free OpenShift project → Liferay welcome page in your
browser. The whole thing takes about 15 minutes plus boot time.

> **Audience.** Anyone who wants to validate the Liferay default Helm chart
> against OpenShift without standing up a real cluster. The Developer Sandbox is
> free, doesn't expire (you re-up it monthly), and runs on the same OpenShift
> bits as the paid product.

> **Order matters.** This guide creates the Route and the `liferay-network`
> ConfigMap _before_ running `helm install`. The ConfigMap injects
> `company.default.virtual.host.name` so that when Liferay initializes its
> database on first boot, the default company is written with the right Route
> hostname from the start. If you install Liferay first and add the ConfigMap
> later, the DB is already populated with the old (default) virtual host and
> you'll fight the data forever.

## Prerequisites on your machine

- `oc` -
  <https://docs.redhat.com/en/documentation/openshift_container_platform/4.8/html/cli_tools/openshift-cli-oc>
- `helm` ≥ 3.8 (needed for OCI registry support).

## 1. Create the Developer Sandbox

1. Go to <https://developers.redhat.com/developer-sandbox> and sign in with a
   Red Hat account (create one for free if you don't have it).
2. Click **Start your sandbox**. The portal provisions an OpenShift project for
   you with a name like `your-username-dev`. Provisioning takes ~1 min.
3. When it lands, click **Launch your Developer Sandbox for Red Hat OpenShift**
   to open the web console.

What you get on the free tier:

| Resource | Limit                           |
| -------- | ------------------------------- |
| Memory   | 7 GiB per project               |
| CPU      | 1 vCPU dedicated (burst higher) |
| Storage  | 5 GiB persistent volumes        |
| Sessions | 30 days (renewable)             |

The values overlay in `cloud/runbooks/openshift/examples/values-openshift.yaml`
is sized to fit inside this 7 GiB ceiling.

## 2. Log in from the CLI

1. In the web console, click your username (top-right) → **Copy login command**.
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

The OpenShift router assigns a hostname the moment the Route resource is created
even before there's a backing Service to forward traffic to. We use that to read
the hostname now, write it into the ConfigMap in step 4, and have Liferay come
up with the right virtual host on first boot.

Apply the Route manifest:

```
oc apply -f cloud/runbooks/openshift/examples/route.yaml
```

See `cloud/runbooks/openshift/examples/route.yaml` for inline reasoning. In
short: edge TLS termination using the cluster's wildcard cert, auto-generated
hostname (Developer Sandbox doesn't allow custom domains on the free tier), HTTP
→ HTTPS redirect.

Read the assigned hostname into a shell variable:

```
ROUTE_HOST="$(oc get route liferay-default -o jsonpath='{.spec.host}')"
echo "$ROUTE_HOST"
```

You'll see something like
`liferay-default-your-username-dev.apps.rm3.7wse.p1.openshiftapps.com`.

> The Route will return 503 until step 5 finishes, the backing Service doesn't
> exist yet.

## 4. Create the `liferay-network` ConfigMap with that hostname

The default chart treats `localhost` as the default company's virtual host.
Hitting Liferay through the Route hostname instead would log
`NoSuchVirtualHostException` on every request. The `liferay-network` ConfigMap
supplies two env vars that fix it:

- `company.default.virtual.host.name` → the Route hostname
- `company.default.virtual.host.sync.on.startup=true` → re-apply on every boot
  (without this, the hostname only takes effect when the default company is
  _first created_)

Edit `cloud/runbooks/openshift/examples/liferay-network-cm.yaml` and replace the
placeholder hostname with the one in `$ROUTE_HOST`.

```
oc apply -f cloud/runbooks/openshift/examples/liferay-network-cm.yaml
```

Verify it landed:

```
oc get configmap liferay-network -o yaml
```

> **Why this ordering matters.** Liferay's `CompanyLocalServiceImpl` writes the
> default company row to the DB during initial bundle activation, using whatever
> value of `company.default.virtual.host.name` is set at that moment. If the
> ConfigMap isn't in place before that first boot, the DB ends up with
> `localhost` and only a retroactive sync (or admin-UI edit) will fix it. Doing
> it now means the data is right from day one.

## 5. Install the Liferay preview chart

The chart is published as an OCI artifact at the cloudnative-team's GHCR
registry:

```
ghcr.io/cloudnative-team/charts-pr/91/liferay-default:0.6.0-pr-91-gaa8311e88
```

Install it with the OpenShift overlay:

```
helm upgrade -i liferay-preview \
 oci://ghcr.io/cloudnative-team/charts-pr/91/liferay-default:0.6.0-pr-91-gaa8311e88 \
    -f cloud/runbooks/openshift/examples/values-openshift.yaml
```

## Explanation of the values-openshift.yaml file

What the overlay does (`cloud/runbooks/openshift/examples/values-openshift.yaml`
has the full reasoning in inline comments):

1. Replaces pinned-UID security contexts so the `restricted-v2` SCC can inject
   `runAsUser` / `fsGroup`.
2. Forces `fsGroupChangePolicy: Always` so a mid-write crash can't corrupt the
   PVC across restarts.
3. References the `liferay-network` ConfigMap from step 4 via `customEnvFrom`,
   so the virtual-host env vars flow into the pod.
4. Bumps the PVC from 1 GiB → 10 GiB (1 GiB fills up before Liferay finishes
   booting).
5. Raises memory limits to 6 GiB to fit Liferay JVM + sidecar Elasticsearch
   without OOMKill.
6. Relaxes startup / readiness / liveness probes to tolerate slower cold-start
   on a constrained cluster.

### Updating values later

If you tweak the overlay, re-render with `helm upgrade`:

```
helm upgrade -i liferay-preview \
  oci://ghcr.io/cloudnative-team/charts-pr/91/liferay-default:0.6.0-pr-91-gaa8311e88 \
  	-f cloud/runbooks/openshift/examples/values-openshift.yaml
```

### Watch the boot

```
oc get pod -w
```

`liferay-default-0` cycles through `Pending` → `Init:…/N` → `PodInitializing` →
`Running 0/1` → `Running 1/1`. First boot takes 5–15 minutes on the sandbox
(sidecar Elasticsearch download + OSGi bundle activation + schema init). The
startup probe budget of 32 min is well past that.

Tail Liferay's own log to see boot progress in real time:

```
oc logs -f liferay-default-0
```

Look for the Liferay banner followed by `Server startup in [...] milliseconds`.

## 6. Verify by logging into Liferay

1. Open `https://$ROUTE_HOST/` in a browser (substitute the actual host). The
   first request will be slow — Liferay populates its virtual-host cache on cold
   hit.
2. The default welcome page should render. Click **Sign In** (top-right).
3. Default credentials for a fresh DXP install:
   - **Email:** `test@liferay.com`
   - **Password:**
     `oc extract secret/liferay-default --keys=LIFERAY_DEFAULT_PERIOD_ADMIN_PERIOD_PASSWORD --to=-`
4. Liferay forces a password reset on first login. Set a new password, then
   accept the terms of use and answer the password reminder prompt.
5. You should land in the Liferay control panel as the default admin. Done.

## Cleanup

Uninstall everything you applied, in reverse order:

```
helm uninstall liferay-preview
oc delete configmap liferay-network
oc delete -f cloud/runbooks/openshift/examples/route.yaml
oc delete pvc liferay-persistent-volume-liferay-default-0
```

The PVC has to be deleted explicitly as Helm leaves it behind on purpose so data
survives an accidental `helm uninstall`. On the sandbox, leaving it behind also
eats into your 5 GiB storage quota.