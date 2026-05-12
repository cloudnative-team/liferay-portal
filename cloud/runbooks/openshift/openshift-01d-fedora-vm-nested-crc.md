# Runbook 01d — Run CRC inside a Fedora VM (escape-hatch for hosts where CRC won't start natively)

**Audience:** anyone whose host distro can't run CRC directly — for example, Arch Linux with an `edk2-ovmf` regression that page-faults during CoreOS boot (see [runbook 01 §Known Arch OVMF regression](./openshift-01-crc-and-helm-install.md#known-arch-ovmf-regression)) — but who needs a full OpenShift cluster with cluster-admin (for Phase B operator installs).

**Scope:** Bring up a Fedora Server VM under libvirt on your host, install CRC inside that VM, validate the chart end-to-end. Same OpenShift experience as native CRC, one layer of virtualization in between.

**Trade-offs:**

| | 01b (k3d) | 01c (Sandbox) | This runbook |
|---|---|---|---|
| Setup time | 5 min | 30 min | 60–90 min first time |
| Cluster-admin? | Yes | No | **Yes** |
| Real OpenShift SCC? | No | Yes | Yes |
| Real OpenShift Routes? | No | Yes | Yes |
| Can install operators? | Yes (K8s) | **No** | **Yes** |
| Resource cost on host | Light | None | ~28 GiB RAM, ~80 GiB disk |

---

## 1. Confirm nested virtualization on the host

```sh
# Intel
cat /sys/module/kvm_intel/parameters/nested
# AMD
cat /sys/module/kvm_amd/parameters/nested
```

Expected `Y` or `1`. If not:

```sh
# Intel
echo "options kvm_intel nested=1" | sudo tee /etc/modprobe.d/kvm-nested.conf
sudo modprobe -r kvm_intel && sudo modprobe kvm_intel
# AMD
echo "options kvm_amd nested=1" | sudo tee /etc/modprobe.d/kvm-nested.conf
sudo modprobe -r kvm_amd && sudo modprobe kvm_amd
```

---

## 2. Confirm libvirt installed (Arch)

```sh
pacman -Q libvirt qemu-desktop dnsmasq virt-manager 2>&1
sudo systemctl is-active libvirtd.socket
id $USER | grep -o 'libvirt'
```

If missing:

```sh
sudo pacman -S --needed libvirt qemu-desktop dnsmasq iptables-nft virt-manager
sudo systemctl enable --now libvirtd.socket virtlogd.socket
sudo usermod -aG libvirt,kvm $USER  # log out + back in
```

---

## 3. Download + resize the Fedora cloud image

```sh
mkdir -p ~/vms/images && cd ~/vms/images
# Pick the current stable from https://fedoraproject.org/cloud/download
curl -LO https://download.fedoraproject.org/pub/fedora/linux/releases/41/Cloud/x86_64/images/Fedora-Cloud-Base-Generic-41-1.4.x86_64.qcow2
qemu-img resize Fedora-Cloud-Base-Generic-41-1.4.x86_64.qcow2 80G
```

---

## 4. Cloud-init seed image

```sh
mkdir -p ~/vms/seed && cd ~/vms/seed

cat > user-data <<'EOF'
#cloud-config
users:
  - name: greg
    sudo: ALL=(ALL) NOPASSWD:ALL
    shell: /bin/bash
    ssh_authorized_keys:
      - PASTE_YOUR_PUBLIC_KEY_HERE
ssh_pwauth: false
EOF

cat > meta-data <<'EOF'
instance-id: crc-host
local-hostname: crc-host
EOF

sed -i "s|PASTE_YOUR_PUBLIC_KEY_HERE|$(cat ~/.ssh/id_ed25519.pub)|" user-data

sudo pacman -S --needed cloud-image-utils 2>/dev/null || sudo pacman -S --needed cdrtools
cloud-localds seed.iso user-data meta-data
```

---

## 5. Define + start the VM

```sh
sudo virt-install \
  --name crc-host \
  --memory 28672 \
  --vcpus 6,sockets=1,cores=6 \
  --cpu host-passthrough \
  --machine q35 \
  --disk path="$HOME/vms/images/Fedora-Cloud-Base-Generic-41-1.4.x86_64.qcow2",format=qcow2 \
  --disk path="$HOME/vms/seed/seed.iso",device=cdrom \
  --os-variant fedora41 \
  --network network=default \
  --graphics none \
  --noautoconsole \
  --import
```

Get the VM IP, SSH in:

```sh
sudo virsh -c qemu:///system net-dhcp-leases default
ssh greg@<VM_IP>
```

Critical: `--cpu host-passthrough` exposes the nested-virt CPU flags. Without it, CRC inside the VM will fall back to TCG and be unusably slow.

---

## 6. Install CRC inside the VM

Inside the VM:

```sh
sudo dnf install -y curl tar
curl -L -o crc.tar.xz https://developers.redhat.com/content-gateway/file/pub/openshift-v4/clients/crc/latest/crc-linux-amd64.tar.xz
tar -xf crc.tar.xz
sudo install crc-linux-*/crc /usr/local/bin/crc
rm -rf crc-linux-* crc.tar.xz
crc version
```

Copy your CRC pull secret in (from the host):

```sh
scp ~/Downloads/pull-secret.txt greg@<VM_IP>:~/
```

Back in the VM:

```sh
crc config set pull-secret-file ~/pull-secret.txt
crc config set cpus 4
crc config set memory 18432
crc config set disk-size 50
crc setup
crc start
eval $(crc oc-env)
oc login -u kubeadmin https://api.crc.testing:6443
```

---

## 7. Browser access from the host (optional)

```sh
# From the Arch host
ssh -L 6443:api.crc.testing:6443 \
    -L 8443:apps-crc.testing:443 \
    greg@<VM_IP>
```

Add to host `/etc/hosts`:
```
127.0.0.1   api.crc.testing
127.0.0.1   liferay-liferay-validate.apps-crc.testing
```

---

## 8. Chart validation

Follow [runbook 01 §4 onwards](./openshift-01-crc-and-helm-install.md#4-create-the-project) — inside the Fedora VM. Everything from there is identical because you're talking to a real CRC instance.

---

## 9. Cleanup

```sh
sudo virsh -c qemu:///system shutdown crc-host
# Or destroy entirely (frees ~30 GiB):
sudo virsh -c qemu:///system destroy crc-host 2>/dev/null
sudo virsh -c qemu:///system undefine crc-host --remove-all-storage
rm -rf ~/vms
```

---

## Troubleshooting

**CRC startup is impossibly slow / qemu CPU pegged forever:** nested KVM isn't actually working. On host: `cat /sys/module/kvm_intel/parameters/nested` must be `Y`. Inside VM: `lscpu | grep -i virt` must show `vmx` or `svm`.

**`crc setup` fails inside Fedora:** unusual (Fedora is CRC's home distro). Check `/dev/kvm` exists inside the VM (`ls -l /dev/kvm`); if missing, the VM wasn't created with `--cpu host-passthrough`.

**Out of memory inside Fedora VM:** bump `crc config set memory` lower and `virt-install --memory` higher.

**SSH won't connect to the VM:** cloud-init may not have finished. Wait 2 min, retry. For boot messages: `sudo virsh -c qemu:///system console crc-host` (Ctrl-] to exit).
