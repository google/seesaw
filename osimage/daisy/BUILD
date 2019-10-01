load("@bazel_tools//tools/build_defs/pkg:pkg.bzl", "pkg_tar")
load("@io_bazel_rules_docker//container:container.bzl", "container_bundle")

OVERLAY_FILES = {
    # From :build step
    "scripts/seesaw-bin-linux-amd64.tar.gz": "seesaw-bin-linux-amd64.tar.gz",
    "scripts/provision-script.sh": "run/provision-script.sh",
    # Configure toolbox.
    "etc_default_toolbox": "etc/default/toolbox",
    # From: extract_toolbox
    "toolbox/LICENSE": "usr/src/toolbox/LICENSE",
    "toolbox/toolbox": "usr/bin/toolbox",
     # Disable network config
    "etc_cloud_cloud_cfg_d_99-disable-network-config_cfg": "etc/cloud/cloud.cfg.d/99-disable-network-config.cfg",
}
pkg_tar(
    name = "overlay",
    # Compatibility with debian packaging
    files = OVERLAY_FILES,
    modes = {
        "seesaw-bin-linux-amd64.tar.gz": "0644",
        "usr/src/toolbox/LICENSE": "0644",
        "etc/default/toolbox": "0644",
    },
)
pkg_tar(
    name = "builddeps",
    files = {
        "@ubuntu_source//file": "ubuntu_source.ova",
        "containers.tar": "containers.tar",
    },
)
genrule(
    name = "extract_toolbox",
    srcs = ["@toolbox//file"],
    outs = [
        "toolbox/LICENSE",
        "toolbox/toolbox",
    ],
    cmd = "tar -C $(@D) -xf $(location @toolbox//file)",
)
container_bundle(
    name = "containers",
    images = {
        "gcr.io/gke-on-prem-release/debug-toolbox:v0.0.1": "@debug_toolbox//image",
    },
)

