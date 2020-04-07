#!/bin/bash
# Exit on error
set -o errexit
# Fail on using unset variables
set -o nounset
# Fail on pipe failure
set -o pipefail
# Inherit error handler
set -o errtrace
# Must not be in trace mode here otherwise the script will abort prematurely.
trap 'echo "BuildFailed: See logs $LINENO"' ERR
set -o xtrace

build_status() {
  set +x
  echo "BuildStatus: $*"
  set -x
}
# Fetch instance attribute
get_instance_attribute() {
  attribute=$1
  url='http://metadata/computeMetadata/v1/instance/attributes'
  curl -f -H 'Metadata-Flavor:Google' "${url}/${attribute}"
}
# Path to sources
DAISY_SOURCES_PATH=$(get_instance_attribute 'daisy-sources-path')
# Target
TARGET_IMAGE_NAME=$(get_instance_attribute 'target-image-name')
IMAGE_VERSION=$(get_instance_attribute 'image-version')
# Docker version to be installed in Host, should be compatible with Target OS
# gsutil cat gs://gke-on-prem-os-ubuntu-source/ubuntu-gke-onprem-1804-1-16-v20200403.manifest | grep docker.io
DOCKER_VERSION=19.03.2-0ubuntu1~18.04.0.1
DOCKER_PPA=docker1903-k8s

# Temporary storage
DAISY_OUTS_PATH=$(get_instance_attribute 'daisy-outs-path')
rootfs=/mnt
rootfs_disk=/dev/sdb
disk_name="${TARGET_IMAGE_NAME}.vmdk"
manifest_name="${TARGET_IMAGE_NAME}.manifest"
rootfs_tarball_name="${TARGET_IMAGE_NAME}.tar.gz"
prepare_host() {
  build_status "Preparing host OS"
  gsutil cp "${DAISY_SOURCES_PATH}/builddeps.tar" ./
  tar --extract --verbose --file builddeps.tar

  # source PPA for required docker version
  add-apt-repository ppa:cloud-images/$DOCKER_PPA -y

  mkdir -p /etc/systemd/system/docker.service.d
  cat > /etc/systemd/system/docker.service.d/gke-docker-options.conf <<'EOF'
# CLOUD_IMG: This file was created/modified by the Cloud Image build process
# This file is overriding parts of docker's packaged service file.
[Service]
ExecStart=
ExecStart=/usr/bin/dockerd -H fd:// --live-restore -s overlay2 $DOCKER_OPTS
EOF

  apt-get update
  env DEBIAN_FRONTEND=noninteractive apt-get install -y \
    qemu-utils \
    docker.io=$DOCKER_VERSION
}
prepare_chroot() {
  build_status "Preparing chroot"

  # Expand main partition to full disk size
  growpart "${rootfs_disk}" 1
  e2fsck -fy "${rootfs_disk}1"
  resize2fs "${rootfs_disk}"1
  sync
  partprobe "${rootfs_disk}"

  # Mount main partition
  mount "${rootfs_disk}1"  "$rootfs" -o noatime
  mount none -t sysfs "${rootfs}/sys"
  mount none -t proc "${rootfs}/proc"
  mount none -t tmpfs "${rootfs}/run"
  mount --bind /dev "${rootfs}/dev"
  mount none -t devpts "${rootfs}/dev/pts"
  # Allow name resolution in chroot through main resolver
  mkdir -p "${rootfs}/run/systemd/resolve"
  touch "${rootfs}/run/systemd/resolve/resolv.conf"
  mount --bind /etc/resolv.conf "${rootfs}/run/systemd/resolve/resolv.conf"
}
provision_image() {
  build_status "Provisioning image"

  # HACK: Embed docker images into filesystem (b/124055335)
  # from builddeps.tar/containers.tar
  systemctl stop docker
  # This would reconfigure docker to use rootfs directory as storage
  echo '{"graph": "'"${rootfs}"'/var/lib/docker"}' > /etc/docker/daemon.json
  systemctl start docker
  docker load < containers.tar
  systemctl stop docker

  # Pull overlay archive
  gsutil cp "${DAISY_SOURCES_PATH}/overlay.tar" "overlay.tar"
  tar --extract --verbose --file overlay.tar --directory "${rootfs}"
  # From overlay.tar
  provision_script_path="/run/provision-script.sh"
  # Run provisioning script inside chroot
  chroot "${rootfs}" env IMAGE_VERSION="$IMAGE_VERSION" \
      bash "${provision_script_path}"
  # Extract the manifest file
  cp "${rootfs}/run/image-manifest.txt" "${manifest_name}"
  # Create a filesystem archive
  tar --create --gzip --one-file-system --directory "${rootfs}" . > "${rootfs_tarball_name}"
}
finalize_chroot() {
  build_status "Unmounting chroot filesystem"
  umount "${rootfs}/run/systemd/resolve/resolv.conf"
  umount "${rootfs}/run"
  umount "${rootfs}/dev/pts"
  umount "${rootfs}/dev"
  umount "${rootfs}/proc"
  umount "${rootfs}/sys"
  umount "${rootfs}"
  sync
}
create_target_image() {
  build_status "Creating target image"
  # streamOptimized creates an immutable image for vCenter
  # compat6 creates VMDK version 6 instead of 4
  qemu-img convert -O vmdk -o subformat=streamOptimized,compat6 \
          "${rootfs_disk}" "${disk_name}"
}
upload_target_image() {
  build_status "Uploading artifacts to ${DAISY_OUTS_PATH}"
  gsutil cp "${disk_name}" "${manifest_name}" "${rootfs_tarball_name}" "${DAISY_OUTS_PATH}/"
}

build_status "Ready for disk. Waiting for target disk to get attached..."

while [ `lsblk | grep disk | wc -l` -lt 2 ]; do
  echo "Only 1 disk found."
  sleep 5
done

build_status "New Disk found: \n `lsblk`"

mkdir -p /build
cd /build
prepare_host
prepare_chroot
provision_image
finalize_chroot
create_target_image
upload_target_image
echo "BuildComplete: Disk image created successfully."
