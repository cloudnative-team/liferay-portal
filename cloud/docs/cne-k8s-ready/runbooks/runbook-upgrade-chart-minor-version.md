---
runbook: upgrade-chart-minor-version
severity: SEV-3 (planned change)
estimated-duration: 30-45 minutes per environment
audience: platform engineer
---

# Upgrade liferay-default Chart (Minor Version)

Upgrade the `liferay-default` Helm chart by a minor version (for example, `0.5.x` → `0.6.x`). This runbook does not cover major-version chart upgrades or DXP image-only upgrades — those have separate runbooks.

## When to Use This Runbook

- Liferay has published a new chart minor version compatible with your DXP image
- Your change-advisory board has approved the upgrade window
- Dev and UAT have been validated with the target version
- The target chart version is not a major-version bump (left-of-decimal change)

## Impact

- Brief reduction in capacity during rolling restart (one of N pods unavailable at a time)
- No data-plane changes: database, search, and object storage are untouched
- Configuration drift surfaces during `helm diff` — review carefully before applying

## Prerequisites

```bash
export NS=<liferay-namespace>
export RELEASE=<helm-release-name>
export CURRENT_VERSION=<current-chart-version>
export NEW_VERSION=<target-chart-version>
export ENV=<dev|uat|production>
```

- UAT must have been upgraded to `$NEW_VERSION` and validated before production
- Database snapshot taken (any DXP-side schema changes need a rollback path)
- Helm 3.8+
- `helm-diff` plugin installed:
  ```bash
  helm plugin install https://github.com/databus23/helm-diff
  ```

## Pre-flight (in dev first, then UAT, then production)

1. Confirm the current chart version in the cluster:
   ```bash
   helm --namespace $NS list -o json | jq -r '.[] | select(.name=="'$RELEASE'") | .chart'
   ```

2. Pull the target chart's defaults and diff against current to spot new keys, removed keys, or changed defaults:
   ```bash
   helm show values \
       oci://us-central1-docker.pkg.dev/liferay-artifact-registry/liferay-helm-chart/liferay-default \
       --version $CURRENT_VERSION > /tmp/values-$CURRENT_VERSION.yaml
   helm show values \
       oci://us-central1-docker.pkg.dev/liferay-artifact-registry/liferay-helm-chart/liferay-default \
       --version $NEW_VERSION > /tmp/values-$NEW_VERSION.yaml
   diff /tmp/values-$CURRENT_VERSION.yaml /tmp/values-$NEW_VERSION.yaml
   ```

3. Run `helm diff` against your live release to preview the rendered change:
   ```bash
   helm --namespace $NS diff upgrade $RELEASE \
       oci://us-central1-docker.pkg.dev/liferay-artifact-registry/liferay-helm-chart/liferay-default \
       --version $NEW_VERSION \
       --values values-$ENV.yaml
   ```

   Pay attention to:
   - Changes to `volumeClaimTemplates.spec` — these fields are immutable after StatefulSet creation
   - Changes to ServiceAccount annotations — may break workload-identity binding
   - Changes to default probes — first-boot timing may shift
   - New required values (the upgrade will fail at template time if any required value is missing)

4. **If the diff includes immutable-field changes**, the chart cannot upgrade in place. Stop and follow the replace-by-recreation path: take a backup, uninstall the release, re-install at the new version, restore data. That is a separate runbook (`runbook-restore-from-backup.md`, planned).

## Remediation (the upgrade)

The preferred path is GitOps: bump the chart version in your environment's Git source-of-truth and let Argo CD or Flux apply. If you operate Helm directly:

1. Apply the upgrade with `--wait` so Helm blocks until pods are ready:
   ```bash
   helm --namespace $NS upgrade $RELEASE \
       oci://us-central1-docker.pkg.dev/liferay-artifact-registry/liferay-helm-chart/liferay-default \
       --version $NEW_VERSION \
       --values values-$ENV.yaml \
       --wait --timeout 30m
   ```

2. In a second terminal, watch the rollout in real time:
   ```bash
   kubectl --namespace $NS rollout status statefulset/liferay-default
   ```

3. If the rollout pauses on a pod that fails to become ready, capture the pod log immediately and skip to **Rollback**:
   ```bash
   kubectl --namespace $NS logs <stuck-pod> > /tmp/$RELEASE-failed-upgrade.log
   ```

## Verification

Run the full set; do not skip steps:

1. All pods report `Ready 1/1`:
   ```bash
   kubectl --namespace $NS get pods -l app.kubernetes.io/name=liferay-default
   ```

2. The chart history records the new revision:
   ```bash
   helm --namespace $NS history $RELEASE
   ```

3. Welcome page loads via the gateway (use a real hostname, not port-forward, so ingress is included in the test).

4. License Manager shows your license still active.

5. Search Connections still report `Active` (Control Panel → Configuration → Search → Connections).

6. A representative read and a representative write succeed end-to-end:
   - Sign in as admin
   - View a public page
   - Upload a small document to a Documents and Media library
   - Sign out

7. Pod logs from the last 10 minutes contain no `ERROR` entries unrelated to user input:
   ```bash
   kubectl --namespace $NS logs statefulset/liferay-default --since=10m | grep -E "ERROR|FATAL"
   ```

## Rollback

```bash
helm --namespace $NS history $RELEASE
helm --namespace $NS rollback $RELEASE <previous-revision> --wait --timeout 30m
kubectl --namespace $NS rollout status statefulset/liferay-default
```

Rollback works for value and template changes. It does **not** rewind:

- Database schema changes Liferay applied on first start of the new version (set `upgrade.database.auto.run=false` in `portal-cloud.properties` if you want to gate this)
- Files written by Liferay to the PVC during the failed upgrade

For schema-touching upgrades, you must restore from the database snapshot taken in **Prerequisites** *and* roll back the chart. Track this as a single change-management activity.

## Escalation

Open a Liferay support case if:

- One or more pods fail repeatedly to become ready after upgrade — SEV-2 if degraded, SEV-1 if all pods are down
- Database schema migration appears stuck (the pod log shows `Running com.liferay.portal.upgrade.*` for more than 30 minutes)
- The `helm diff` showed an immutable-field change you did not expect

Attach a [support bundle](./runbook-collect-support-bundle.md) and the failed-upgrade log captured during remediation.

## Related

- [collect-support-bundle](./runbook-collect-support-bundle.md)
- `runbook-restore-from-backup.md` — when rollback alone is insufficient (planned)
- `runbook-upgrade-dxp-image.md` — image-only upgrades (planned)
