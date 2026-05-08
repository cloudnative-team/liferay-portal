---
runbook: license-expiring
severity: SEV-3 (planned) / SEV-2 (under 72h to expiry) / SEV-1 (already expired)
estimated-duration: 15 minutes
audience: platform engineer + Liferay administrator
---

# Liferay License Expiring

Replace the Liferay activation key before it expires (or recover after expiry).

## When to Use This Runbook

- A monitoring check on the license expiration date fires (recommended thresholds: 30, 14, 7, 3 days)
- A user-visible warning banner appears in the Liferay admin UI
- Pod logs contain `License will expire on...` warnings
- The license has already expired and EE-only features have stopped working

## Impact

- License *nearing* expiry: cosmetic warning, no functional impact yet — treat as SEV-3
- Under 72 hours to expiry: treat as SEV-2; renewal must be in motion
- Already expired: depending on license type, admin actions and EE-only features are blocked; site-delivery may continue but is not guaranteed — treat as SEV-1

## Prerequisites

```bash
export NS=<liferay-namespace>
export RELEASE=<helm-release-name>
```

- New `license.xml` activation key from your Liferay account manager
- Write access to the secrets vault holding the `liferay-license` Secret (or its source)
- `kubectl` access to `$NS`

## Diagnostic Steps

1. Confirm the current license expiration as Liferay sees it. Sign in as admin and navigate to *Control Panel → Configuration → License Manager*. Note the expiration date and product key type.

2. Confirm the on-disk license file in the pod matches:
   ```bash
   kubectl --namespace $NS exec statefulset/liferay-default -- \
       find /etc/liferay/mount/files/deploy -name "license*.xml" \
       -exec sh -c 'grep -E "<end-date>|<product-key-package-id>" {}' \;
   ```

3. Confirm when the Kubernetes Secret was last updated:
   ```bash
   kubectl --namespace $NS get secret liferay-license \
       -o jsonpath='{.metadata.annotations}' | jq
   ```

## Remediation

1. Update the license content in your secrets vault. The Kubernetes-Ready path expects the license to live in the source-of-truth vault; the Kubernetes Secret is a synced copy.

   AWS Secrets Manager example:
   ```bash
   aws secretsmanager update-secret \
       --secret-id liferay/licenses/<license-name> \
       --secret-string "$(base64 -w 0 new-license.xml)"
   ```

   For HashiCorp Vault, Azure Key Vault, GCP Secret Manager — use the equivalent update primitive.

2. If using External Secrets Operator, force a sync rather than waiting for the next interval:
   ```bash
   kubectl --namespace $NS annotate externalsecret liferay-license \
       force-sync=$(date +%s) --overwrite
   ```

   For Vault Agent injector, restart the pods (step 4) — the agent re-renders on pod startup.

3. Confirm the Kubernetes Secret picked up the new content:
   ```bash
   kubectl --namespace $NS get secret liferay-license \
       -o jsonpath='{.data.license\.xml}' | base64 -d | grep -E "<end-date>|<product-key-package-id>"
   ```

4. Trigger a rolling restart so each pod re-reads the mounted license file. The license is read at startup; running pods do not pick up file changes:
   ```bash
   kubectl --namespace $NS rollout restart statefulset/liferay-default
   kubectl --namespace $NS rollout status statefulset/liferay-default
   ```

## Verification

1. All pods report `Ready 1/1` after rollout.

2. License Manager UI shows the new expiration date.

3. Pod logs from the last 10 minutes contain `License registered successfully` for the new license:
   ```bash
   kubectl --namespace $NS logs statefulset/liferay-default --since=10m | grep -i license
   ```

4. No `LicenseException` or `LicenseManagerException` entries in the same window.

## Rollback

If the new license fails to activate — typically a corrupted file or wrong product code:

1. Restore the previous license value in the vault (most vault providers retain version history).
2. Force-sync the ExternalSecret as in step 2 above.
3. Rolling restart as in step 4.
4. Verify as above.

You will be back to the "expiring soon" state. Open a Liferay support case for an emergency replacement license.

## Escalation

- **30 days before expiry:** contact your Liferay account manager to begin renewal. License generation can take several business days.
- **License already expired:** open a SEV-1 support case immediately. Reference the case ID in your change-management ticket for the rolling restart.
- **License activates but EE features still blocked:** open a SEV-2 case with a [support bundle](./runbook-collect-support-bundle.md) attached.

## Related

- [collect-support-bundle](./runbook-collect-support-bundle.md)
- Liferay docs: *Activating Liferay DXP*
