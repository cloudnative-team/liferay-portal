# Runbook 01 — Validate `liferay-default` on OpenShift Local (CRC) for local testers

**Audience:** Liferay maintainers and contributors validating chart changes on a local OpenShift Local (CRC) cluster before publishing.

**Scope:** smoke test only. Stand up a local single-node OpenShift cluster, install the chart with the OpenShift `values-openshift.yaml` overlay, add a `Route`, and verify the Liferay pod is admitted and starts. Database, search, and object storage are **not** wired up in this runbook — Liferay will fail to fully start until those are configured. The signal here is "SCC and admission accept the rendered chart and the JVM begins to start."

If CRC won't run on your host (e.g. recent Arch `edk2-ovmf` regression — see [§Known Arch OVMF regression](#known-arch-ovmf-regression)), use one of:
- [`openshift-01b-k3d-chart-validation.md`](./openshift-01b-k3d-chart-validation.md) — k3d substitute
- [`openshift-01c-developer-sandbox.md`](./openshift-01c-developer-sandbox.md) — Red Hat Developer Sandbox
- [`openshift-01d-fedora-vm-nested-crc.md`](./openshift-01d-fedora-vm-nested-crc.md) — Fedora VM running nested CRC

For customer-facing OpenShift deploys (not testing), see [`customer-openshift-deploy.md`](./customer-openshift-deploy.md).

---

## 1. Prerequisites

| Requirement | Notes |
|---|---|
| Linux/macOS/Windows host with virtualization | CRC runs an OpenShift single-node cluster in a local VM. |
| 6+ CPU available, 24+ GiB RAM available, 50+ GiB free disk | Default CRC config is 4 CPU/9 GiB; Liferay's pod alone requests ~6 GiB. Bump CRC to fit. |
| Red Hat Developer account (free) | Needed to download the CRC pull secret. https://developers.redhat.com |
| `helm` v3+ on PATH | https://helm.sh/docs/intro/install/ |

---

## 2. Install OpenShift Local (CRC)

Download from https://console.redhat.com/openshift/create/local.

On Linux:

```sh
tar -xf crc-linux-amd64.tar.xz
sudo install crc-linux-*/crc /usr/local/bin/crc
crc version
```

Download your CRC pull secret from the same page (file is `pull-secret.txt`).

### 2a. Linux distribution notes

`crc setup` auto-installs libvirt on Fedora/RHEL (`dnf`) and Debian/Ubuntu (`apt`). On other distros it prints `unsupported distribution ... trying to install libvirt with dnf` and fails. Pre-install libvirt manually, then re-run `crc setup` — it'll detect the existing packages and skip the install step.

**Arch Linux:**

```sh
sudo pacman -S --needed libvirt qemu-desktop dnsmasq iptables-nft virt-manager
sudo systemctl enable --now libvirtd.socket virtlogd.socket
sudo usermod -aG libvirt,kvm $USER
# log out + back in so the group takes effect (or `newgrp libvirt` for this shell)
virsh -c qemu:///system list   # smoke check — should print an empty table, not a perms error

# CRC's user-level crc-vsock.socket listens on AF_VSOCK; vhost_vsock isn't
# loaded by default on Arch. Load it now and persist it.
sudo modprobe vhost_vsock
echo vhost_vsock | sudo tee /etc/modules-load.d/vhost_vsock.conf
lsmod | grep vsock             # expect vhost_vsock plus vmw_vsock_* + vsock

crc setup                      # re-run
```

**Debugging tip:** if `crc setup` reports `Executing systemctl action failed ... Job failed. See "journalctl -xe" for details.`, look in the **user** journal (CRC's units live there), not the system one: `journalctl --user -xe`.

### <a name="known-arch-ovmf-regression"></a>Known Arch OVMF regression

Some recent `edk2-ovmf` releases ship an OVMF binary that page-faults during CoreOS boot under CRC's libvirt domain. Symptom inside `crc start`: an SSH-handshake retry loop ("read tcp ... connection reset by peer"); qemu pegs CPU at ~100% but the VM never reaches Linux. Confirm by checking `sudo tail /var/log/libvirt/qemu/crc.log` — if you see `!!!! X64 Exception Type - 0E(#PF - Page-Fault)` right after `Booting Red Hat Enterprise Linux CoreOS …`, this is the bug. Both the secboot and non-secboot variants in the affected release are impacted, so swapping variants doesn't help.

Workarounds: downgrade `edk2-ovmf` via the [Arch Linux Archive](https://archive.archlinux.org/packages/e/edk2-ovmf/), or skip native CRC and use [runbook 01d (Fedora VM + nested CRC)](./openshift-01d-fedora-vm-nested-crc.md).

---

## 3. Configure and start CRC

```sh
crc config set cpus 6
crc config set memory 24576           # 24 GiB
crc config set disk-size 60           # 60 GiB
crc config set pull-secret-file ~/Downloads/pull-secret.txt
crc setup
crc start
```

First start: ~10–15 minutes; prints a `kubeadmin` password at the end — save it.

```sh
eval $(crc oc-env)
oc version
oc login -u kubeadmin https://api.crc.testing:6443
```

The cluster's apps domain is `apps-crc.testing`. Routes auto-generate hostnames there.

---

## 4. Create the project

```sh
oc new-project liferay-validate
```

Use `oc new-project` (not `oc create namespace`) — it adds the SCC UID-range annotations OpenShift needs.

```sh
oc get namespace liferay-validate -o jsonpath='{.metadata.annotations.openshift\.io/sa\.scc\.uid-range}'; echo
```

You'll see something like `1000700000/10000`. OpenShift will assign Liferay a UID inside that range — the `values-openshift.yaml` overlay you'll write in §5 supplies a non-pinned security context so the SCC has room to inject.

---

## 5. OpenShift values overlay

Copy the chart-shipped example and tighten its resources for a local CRC smoke install:

```sh
cp /home/greg/repos/liferay/liferay-portal/cloud/helm/default/examples/values-openshift.yaml ./
cat >> values-openshift.yaml <<'EOF'

# Tighten resources for local CRC. Chart default asks 6 GiB / 2 vCPU.
resources:
    requests:
        cpu: 500m
        memory: 2Gi
    limits:
        cpu: 2000m
        memory: 4Gi
EOF
```

DB/ES/S3 are intentionally absent — Liferay will fail to fully start without them, which is the expected smoke-test signal.

---

## 6. Install the chart

From a published preview repo:

```sh
helm repo add liferay-preview <PREVIEW_REPO_URL>
helm repo update
helm install liferay liferay-preview/liferay-default \
  --version <PREVIEW_CHART_VERSION> \
  --namespace liferay-validate \
  --values ./values-openshift.yaml
```

Or from a local checkout (typical for chart-development testing):

```sh
helm install liferay /home/greg/repos/liferay/liferay-portal/cloud/helm/default \
  --namespace liferay-validate \
  --values ./values-openshift.yaml
```

Watch:

```sh
oc -n liferay-validate get pods -w
```

`Ctrl-C` once the pod reaches `Running` (or settles into a probe-failure CrashLoop without a DB — expected).

---

## 7. Add the OpenShift Route

The chart doesn't ship a `Route` template, but it does ship a ready-to-apply manifest:

```sh
oc -n liferay-validate apply \
  -f /home/greg/repos/liferay/liferay-portal/cloud/helm/default/examples/route.yaml
```

See [customer-openshift-deploy.md §5](./customer-openshift-deploy.md#5-add-an-openshift-route) for the wrapper-chart alternative.

---

## 8. Validate

**8a. Pod admitted; SCC injected a UID:**

```sh
oc -n liferay-validate get pod liferay-default-0 \
  -o jsonpath='{.spec.containers[0].securityContext}{"\n"}{.spec.securityContext}{"\n"}'
oc -n liferay-validate get pod liferay-default-0 \
  -o jsonpath='{.metadata.annotations.openshift\.io/scc}{"\n"}'
```

Expected: numeric `runAsUser` in the namespace range; SCC `restricted-v2` (or newer).

**8b. No SCC denial events:**

```sh
oc -n liferay-validate get events --sort-by=.lastTimestamp | grep -iE 'forbidden|violates' || echo "no SCC denials"
```

**8c. Init containers ran; main container started:**

```sh
oc -n liferay-validate describe pod liferay-default-0 | sed -n '/Init Containers/,/^Conditions/p'
oc -n liferay-validate logs liferay-default-0 -c liferay-default --tail=80
```

Expected in logs: the Tomcat/Liferay startup banner, OSGi bundles loading, then JDBC connection failure (DB not wired — that's the success criterion).

**8d. Route reachable:**

```sh
ROUTE_HOST=$(oc -n liferay-validate get route liferay -o jsonpath='{.spec.host}')
curl -sk -I "https://$ROUTE_HOST" | head -3
```

Any HTTP response = the router forwarded to your pod. A 502/503 is fine here; we're only checking the routing plumbing.

---

## 9. Cleanup

```sh
helm -n liferay-validate uninstall liferay
oc delete route liferay -n liferay-validate
oc delete project liferay-validate
```

To stop CRC without removing it: `crc stop`. To remove it entirely: `crc delete && crc cleanup`.

---

## 10. Common failure modes

| Symptom | Likely cause | Fix |
|---|---|---|
| `forbidden: unable to validate against any security context constraint` | The overlay's `securityContext` / `podSecurityContext` weren't applied; the chart fell back to pinned UID 1000. | Re-render with `helm template ... --values ./values-openshift.yaml` and confirm `runAsUser` is absent from the rendered pod. |
| Pod stuck `Pending` — `Insufficient memory` | CRC running with default 9 GiB. | `crc stop && crc config set memory 24576 && crc start` |
| `liferay-prepopulate-data` init fails with `Permission denied` writing to `/temp/liferay` | PVC from a previous run owned by a different UID. | `oc -n liferay-validate delete pvc -l app=liferay-default` then reinstall. |
| `helm install` fails with `unknown` Route fields | OpenShift Route CRD missing — not really OpenShift, or you applied the manifest before login. | `oc api-resources \| grep route.openshift.io` should list it. |
| Route returns OpenShift's default 503 page | Service has no ready endpoints. | `oc -n liferay-validate get endpoints liferay-default` — empty means readiness probe is still failing. Check logs. |

---

## 11. Next

Once this smoke install passes, the chart is admission-clean on real OpenShift. Production-grade next steps:
- Wire Postgres (CloudNativePG / Crunchy), Elasticsearch (ECK), MinIO, ESO — see future `openshift-02-backing-services.md`.
- For customer-facing deploy: [`customer-openshift-deploy.md`](./customer-openshift-deploy.md).
