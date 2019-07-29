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
echo "IMAGE_VERSION=$IMAGE_VERSION"
cat >> /etc/cloud/build.info <<EOF
gke_on_prem_version: $IMAGE_VERSION
EOF
apt-get update -y
apt-get install -y \
  prips \
  cloud-utils \
  conntrack
# Disable swap otherwise kubelet won't run.
sed -i '/ swap / s/^/#/' /etc/fstab
# Don't wait network to be online. The network could be configured in
# start script.
systemctl disable systemd-networkd-wait-online.service
# Add folder for Kubernetes certificate.
mkdir -p /etc/kubernetes/pki
# Create system user for SSH tunneling
SSH_TUNNEL_USER=gke-apiserver-tunnel
mkdir -p /var/lib/gke
useradd --comment 'GKE Apiserver Tunnel' \
  --home-dir "/var/lib/gke/${SSH_TUNNEL_USER}" --create-home \
  --shell '/usr/sbin/nologin' --system \
  "${SSH_TUNNEL_USER}"
usermod --lock "${SSH_TUNNEL_USER}"
cat >>/etc/ssh/sshd_config <<EOF
#### Google GKE SSH Tunnel User control. Do not edit this section.
Match User ${SSH_TUNNEL_USER}
  AllowTcpForwarding local
  AllowStreamLocalForwarding no
  X11Forwarding no
  PermitTunnel no
  PermitTTY no
  GatewayPorts no
  AllowAgentForwarding no
  ForceCommand echo 'This account can only be used for ssh tunnels'
#### End Google GKE SSH Tunnel User control.
EOF
apt-get clean
dpkg-query -W > /run/image-manifest.txt
