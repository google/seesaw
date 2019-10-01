#!/bin/bash
# Provision the target VM.
# This script runs within chroot
# Usage:
#
# IMAGE_VERSION=20180101-aabbccdd \
#   ./provision-script.sh
set -o xtrace
set -o errexit
set -o pipefail
set -o nounset

tar --extract --no-overwrite-dir --verbose --file /seesaw-bin-linux-amd64.tar.gz --directory /

echo "IMAGE_VERSION=$IMAGE_VERSION"
cat >> /etc/cloud/build.info <<EOF
gke_on_prem_version: $IMAGE_VERSION
EOF
apt-get update -y
apt-get install -y \
  arping \
  cloud-utils \
  conntrack \
  logrotate \
  prips \
  systemd-container

# Disable swap otherwise kubelet won't run.
sed -i '/ swap / s/^/#/' /etc/fstab
# Don't wait network to be online. The network could be configured in
# start script.
systemctl disable systemd-networkd-wait-online.service
# Add folder for Kubernetes certificate.
mkdir -p /etc/kubernetes/pki

# prepare Seesaw environment
apt-get install -y \
  libnl-3-dev \
  libnl-genl-3-dev \
  ipvsadm \

# Seesaw requires a dummy interface
cat > /etc/systemd/network/10-dummy0.netdev <<EOF
[NetDev]
Name=dummy0
Kind=dummy
EOF

echo "ip_vs" > /etc/modules-load.d/ip_vs.conf
echo "options ip_vs  conn_tab_bits=20" > /etc/modprobe.d/ip_vs.conf
echo "nf_conntrack_ipv4" > /etc/modules-load.d/nf_conntrack_ipv4.conf
echo "dummy" > /etc/modules-load.d/dummy.conf
echo "options dummy numdummies=1" > /etc/modprobe.d/dummy.conf

apt-get clean
# Remove machine identifier if systemd sets one up in a
# postinstall script.
truncate -s 0 /etc/machine-id
dpkg-query -W > /run/image-manifest.txt
