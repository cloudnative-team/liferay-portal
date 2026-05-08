---
runbook: collect-support-bundle
severity: any (utility)
estimated-duration: 5 minutes
audience: anyone opening a Liferay support case
---

# Collect a Liferay on Kubernetes Support Bundle

Capture the cluster, chart, and pod state that Liferay support needs to diagnose Kubernetes Ready deployments. Run this before opening any support case.

## When to Use This Runbook

- Opening a Liferay support case for a Kubernetes Ready deployment
- Engaging Liferay engineering on an architectural review
- Internal incident postmortem

## Prerequisites

```bash
export NS=<liferay-namespace>
export RELEASE=<helm-release-name>
export OUT=liferay-support-bundle-$(date +%Y%m%d-%H%M%S)
```

- `kubectl` read access to `$NS`
- `helm` 3.8+
- A way to attach files to your support case (most cases accept tarballs up to ~50 MB)

## Procedure

1. Create the output directory:
   ```bash
   mkdir -p "$OUT"
   ```

2. Capture cluster and chart metadata:
   ```bash
   kubectl version > "$OUT/kubectl-version.txt" 2>&1
   helm version > "$OUT/helm-version.txt" 2>&1
   helm --namespace $NS list -o yaml > "$OUT/helm-list.yaml"
   helm --namespace $NS get values $RELEASE > "$OUT/helm-values.yaml"
   helm --namespace $NS get manifest $RELEASE > "$OUT/helm-manifest.yaml"
   helm --namespace $NS history $RELEASE > "$OUT/helm-history.txt"
   ```

3. Capture Kubernetes resource state:
   ```bash
   kubectl --namespace $NS get all -o yaml > "$OUT/k8s-resources.yaml"
   kubectl --namespace $NS describe statefulset liferay-default > "$OUT/sts-describe.txt"
   kubectl --namespace $NS get events --sort-by=.lastTimestamp > "$OUT/events.txt"
   kubectl --namespace $NS get pvc -o yaml > "$OUT/pvcs.yaml"
   kubectl --namespace $NS get configmaps -o yaml > "$OUT/configmaps.yaml"
   ```

4. Capture pod logs (current and previous on each replica):
   ```bash
   for pod in $(kubectl --namespace $NS get pods \
       -l app.kubernetes.io/name=liferay-default -o name); do
       name=$(basename $pod)
       kubectl --namespace $NS logs $pod --all-containers \
           > "$OUT/log-$name-current.txt" 2>&1
       kubectl --namespace $NS logs $pod --all-containers --previous \
           > "$OUT/log-$name-previous.txt" 2>&1 || true
       kubectl --namespace $NS describe $pod > "$OUT/describe-$name.txt"
   done
   ```

5. Capture Secret *names only* (never values):
   ```bash
   kubectl --namespace $NS get secrets -o name > "$OUT/secret-names.txt"
   ```

6. Sanitize before bundling. The bundle should contain no credentials, no license content, no private keys:
   ```bash
   grep -rE '(password|secret|token|key|license|BEGIN.*PRIVATE)' "$OUT" \
       --include="*.yaml" --include="*.txt" \
       | grep -vE '(secretRef|secretName|name:.*secret|configMapKeyRef|labels)' \
       > "$OUT/sanitization-review.txt"
   ```
   Review `sanitization-review.txt`. Any line that contains an actual credential value must be redacted in the source file before continuing.

7. Bundle and clean up:
   ```bash
   tar czf "$OUT.tar.gz" "$OUT"
   rm -rf "$OUT"
   ```

8. Attach `$OUT.tar.gz` to the support case.

## What is *not* in the bundle (and why)

- **Secret values** — credentials never leave your cluster. If Liferay support needs to validate a specific Secret structure, they will request it through a secure channel.
- **PVC contents** — runtime caches and OSGi resolution state are reproducible; not useful for diagnosis and risk leaking content.
- **Database rows or search-index documents** — schema state can be requested separately if needed; raw content is not.
- **JVM heap dumps** — these are large and may contain sensitive data. Capture them on demand per [pod-oomkilled](./runbook-pod-oomkilled.md) and upload through a secure channel only when support requests them.

## Caveats

- **ConfigMap content is included.** The Liferay chart's ConfigMap holds OSGi configurations referencing `$[env:...]` placeholders, which are safe. If your team has put credentials in any other ConfigMap (an anti-pattern), the sanitization step will catch them — review the output carefully.
- **`helm get values` output may include placeholders.** If you have inlined credentials directly in `values.yaml` (also an anti-pattern; use Secrets instead), redact them before submitting.
- **Tarball size** — if the bundle exceeds your support portal's upload limit, drop the `--previous` log capture in step 4, or split per-pod logs into a separate upload.

## Related

- [pod-oomkilled](./runbook-pod-oomkilled.md)
- [upgrade-chart-minor-version](./runbook-upgrade-chart-minor-version.md)
- [Architecture and Sizing](../architecture-and-sizing.md) — for the support model
