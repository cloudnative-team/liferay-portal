---
taxonomy-category-names:
  - Cloud
  - DXP Self-Hosted Installation, Maintenance, and Administration
  - Liferay Self-Hosted
  - Liferay PaaS
uuid: TBD
---

# CNE Kubernetes Ready: Runbook Catalog

This folder holds operational runbooks for Liferay DXP deployments on the Kubernetes Ready path. Each runbook covers exactly one operational scenario — alert response, planned change, or recovery procedure — and is meant to be readable in under five minutes by an on-call engineer who has never seen the system before.

This catalog is the missing layer between the [onboarding guide](../cne-k8s-ready.md) (how to install) and live operations (how to run). Mature ISVs ship 20-50 of these; this folder is the seed.

## Runbook Format

Every runbook in this folder follows the same shape so on-call readers always know where to find each section:

```text
---
runbook: <kebab-case-id>
severity: <SEV-N or "any">
estimated-duration: <range>
audience: <role>
---

# Title

## When to Use This Runbook   ← triggers, alert names, symptoms
## Impact                      ← user-facing and operational
## Prerequisites               ← access, tooling, prior steps
## Diagnostic Steps            ← read-only commands to confirm scope
## Remediation                 ← numbered, branchable when needed
## Verification                ← how to know it worked
## Rollback                    ← how to undo, when applicable
## Escalation                  ← when to call Liferay support
## Related                     ← links to neighboring runbooks
```

Format rules:

- **One scenario per file.** If you find yourself adding a second `## When to Use This Runbook` heading, split into two files.
- **Read-only steps come before mutating ones.** Diagnostics never modify state.
- **Every command is paste-ready.** Variables are declared at the top of the procedure (`export NS=<namespace>`); inline placeholders are `<like-this>`.
- **No prose that doesn't help the reader act.** Save background and theory for `architecture-and-sizing.md` or `cne-k8s-ready.md`.

## Runbook Catalog

### Drafted (templates — use as patterns for the rest)

| Runbook | Trigger | Type |
| :--- | :--- | :--- |
| [pod-oomkilled](./runbook-pod-oomkilled.md) | Alert: container restarted with `OOMKilled` | Incident response |
| [license-expiring](./runbook-license-expiring.md) | Calendar: 30/14/7/3-day expiry warning | Planned change |
| [upgrade-chart-minor-version](./runbook-upgrade-chart-minor-version.md) | Change-window: new chart minor released | Planned change |
| [collect-support-bundle](./runbook-collect-support-bundle.md) | Any case opened with Liferay support | Diagnostic |

### Planned (next round)

| Runbook | Trigger |
| :--- | :--- |
| `runbook-pod-pending.md` | Alert: pod scheduling stuck |
| `runbook-search-cluster-degraded.md` | Alert: Liferay search connection unhealthy |
| `runbook-database-connection-saturation.md` | Alert: Hikari pool > 80% utilized |
| `runbook-pvc-disk-full.md` | Alert: PVC > 85% full |
| `runbook-rotate-database-password.md` | Calendar / compliance |
| `runbook-rotate-tls-certificate.md` | Calendar / pre-expiry |
| `runbook-restore-from-backup.md` | Incident: data corruption / loss |
| `runbook-drain-node-for-maintenance.md` | Planned: node patching |
| `runbook-scale-replicas.md` | Planned: capacity change |
| `runbook-upgrade-dxp-image.md` | Planned: DXP version bump |
| `runbook-add-osgi-module.md` | Planned: deploy a CX or OSGi extension |
| `runbook-recover-cluster-link-membership.md` | Incident: split-brain or stuck members |
| `runbook-failover-database.md` | Incident: primary DB unhealthy |

## Conventions

- **Severity** uses the four-tier model: SEV-1 (full outage), SEV-2 (major degradation), SEV-3 (planned change or minor degradation), SEV-4 (informational).
- **Variables in commands.** Every runbook declares `$NS` (namespace) and `$RELEASE` (Helm release name) at the top. Reuse those everywhere; do not re-declare per command.
- **GitOps assumption.** Most runbooks assume changes flow through a GitOps controller. When direct `kubectl edit` or `helm upgrade` is the right answer (incident response), the runbook says so explicitly.
- **No vendor-specific assumptions outside the chart.** Runbooks reference your secrets vault, your ingress, your monitoring abstractly. Concrete vendor commands appear only as examples in code comments.
