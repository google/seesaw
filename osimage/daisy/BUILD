load("@bazel_tools//tools/build_defs/pkg:pkg.bzl", "pkg_tar")
OVERLAY_FILES = {
    # From :build step
    "scripts/seesaw-bin-linux-amd64.tar.gz": "seesaw-bin-linux-amd64.tar.gz",
    "scripts/provision-script.sh": "run/provision-script.sh",
}
pkg_tar(
    name = "overlay",
    # Compatibility with debian packaging
    files = OVERLAY_FILES,
    modes = {
        "seesaw-bin-linux-amd64.tar.gz": "0644",
    },
)
pkg_tar(
    name = "builddeps",
    files = {
        "@ubuntu_source//file": "ubuntu_source.ova",
    },
)
