---
runbook: pod-oomkilled
severity: SEV-2 (multi-replica) / SEV-1 (single replica)
estimated-duration: 15-30 minutes
audience: on-call platform engineer
---

# Liferay Pod OOMKilled

A `liferay-default-*` pod was terminated by the kernel for exceeding its memory limit, or by the JVM for exhausting heap.

## When to Use This Runbook

- An alert fired on `kube_pod_container_status_last_terminated_reason{reason="OOMKilled"}` for the Liferay namespace
- `kubectl get pods` shows recent restarts on `liferay-default-*` with `Reason: OOMKilled`
- Users report intermittent 5xx errors and pod logs end abruptly

## Impact

- One pod is unavailable during restart (5-10 minutes for Liferay first-time-on-this-node startup)
- Multi-replica deployments degrade but stay serving via remaining pods
- Single-replica deployments (dev / sandbox) are fully down during restart
- In-flight HTTP sessions on the affected pod are lost; cluster-link recovers them on remaining pods if `replicaCount > 1`

## Prerequisites

```bash
export NS=<liferay-namespace>
export POD=<affected-pod-name>
```

- `kubectl` access to `$NS`
- Read access to your Prometheus / metrics backend
- For permanent fixes: write access to the GitOps repo holding `values-<env>.yaml`

## Diagnostic Steps

1. Confirm the OOMKill and capture the exit code:
   ```bash
   kubectl --namespace $NS describe pod $POD | grep -A2 "Last State"
   ```
   Look for `Reason: OOMKilled`, `Exit Code: 137`.

2. Check whether this is a one-off or a pattern:
   ```bash
   kubectl --namespace $NS get events --field-selector reason=OOMKilling --sort-by=.lastTimestamp
   ```
   Pull `increase(kube_pod_container_status_restarts_total{namespace="$NS"}[24h])` from Prometheus for trend.

3. Determine which OOM occurred. Pull the 30 minutes before the restart from Prometheus:
   - `container_memory_working_set_bytes{pod="$POD"}` — pod memory
   - `jvm_memory_used_bytes{pod="$POD",area="heap"}` — JVM heap used
   - `jvm_memory_max_bytes{pod="$POD",area="heap"}` — JVM heap max

   - **JVM heap OOM**: heap-used approaches heap-max, then the JVM exits cleanly with `OutOfMemoryError` in the pod log
   - **Container memory OOM**: working-set approaches the pod memory limit, then the kernel SIGKILLs the JVM with no heap dump

4. If the previous container's log is still available, capture it before it rotates out:
   ```bash
   kubectl --namespace $NS logs $POD --previous > /tmp/$POD-previous.log
   ```

## Remediation

### Branch A — One-off, no pattern

Watch the pod recover and file a ticket to investigate root cause. **Do not change resources reflexively** — single-instance OOMs are often triggered by a specific request pattern that should be diagnosed before the system grows.

```bash
kubectl --namespace $NS rollout status statefulset/liferay-default
```

### Branch B — Recurring container memory OOM (working-set hits the limit)

Raise pod memory by ~25%, keeping JVM heap unchanged so off-heap headroom grows. Edit `values-<env>.yaml`:

```yaml
resources:
    requests:
        memory: 20Gi   # was 16Gi
    limits:
        memory: 30Gi   # was 24Gi
```

Commit and let your GitOps controller apply. Do not `kubectl edit` the StatefulSet directly — the change reverts on next reconcile.

### Branch C — Recurring JVM heap OOM (heap exhausted)

Raise heap (`-Xmx`) by ~25%. **Always raise pod memory by at least the same amount** so off-heap room is preserved.

```yaml
customEnv:
    x-liferay-jvm:
        -   name: LIFERAY_JVM_OPTS
            value: "-Xms15g -Xmx15g -XX:+UseG1GC -XX:MaxGCPauseMillis=200"

resources:
    requests:
        memory: 24Gi
    limits:
        memory: 32Gi
```

If heap OOM recurs after a 50% heap increase from the [Medium-tier baseline](../architecture-and-sizing.md#sizing-tiers), capture a heap dump for analysis by adding to `LIFERAY_JVM_OPTS`:

```
-XX:+HeapDumpOnOutOfMemoryError -XX:HeapDumpPath=/opt/liferay/data/heap-dumps/
```

The path is on the PVC, so the dump survives pod restart.

## Verification

1. All pods report `Ready 1/1`:
   ```bash
   kubectl --namespace $NS get pods -l app.kubernetes.io/name=liferay-default
   ```

2. No new `OOMKilling` events in the last 30 minutes:
   ```bash
   kubectl --namespace $NS get events --field-selector reason=OOMKilling \
       --sort-by=.lastTimestamp | tail
   ```

3. Pod memory working set stays below 80% of the new limit during normal load (verify in Prometheus).

## Rollback

If raising memory caused worse symptoms — typically because the cluster lacks node capacity for the larger pod — revert the `values-<env>.yaml` change. Pods resize back on next rollout.

## Escalation

Open a Liferay support case if:

- Heap OOM recurs after raising heap 50%+ above the Medium-tier baseline
- The heap dump shows large retained graphs from `com.liferay.*` packages (suggests a Liferay-internal leak)
- You cannot reproduce the OOM in UAT with the same content and load profile

Attach a [support bundle](./runbook-collect-support-bundle.md).

## Related

- [collect-support-bundle](./runbook-collect-support-bundle.md)
- [Architecture and Sizing](../architecture-and-sizing.md) — JVM heap vs. pod memory ratio
