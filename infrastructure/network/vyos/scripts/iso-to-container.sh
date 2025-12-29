#!/bin/bash
#
# iso-to-container.sh - Convert a VyOS ISO to a Docker container image
#
# Usage: iso-to-container.sh <iso-path> [image-name:tag]
#
# This script extracts the squashfs filesystem from a VyOS ISO and builds
# a Docker container image suitable for use with Containerlab.
#
# Requirements:
#   - 7z (p7zip-full package)
#   - sqfs2tar (squashfs-tools-ng package)
#   - docker
#

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DOCKERFILE="${SCRIPT_DIR}/../Dockerfile.containerlab"

# Cache directory for rootfs.tar (set by labctl via LABCTL_HOOK_CACHE)
CACHE_DIR="${LABCTL_HOOK_CACHE:-}"

usage() {
    echo "Usage: $0 <iso-path> [image-name:tag]"
    echo ""
    echo "Converts a VyOS ISO to a Docker container image."
    echo ""
    echo "Arguments:"
    echo "  iso-path      Path to VyOS ISO file"
    echo "  image-tag     Docker image name:tag (default: vyos-gateway:test)"
    echo ""
    echo "Requirements:"
    echo "  - 7z (p7zip-full package)"
    echo "  - sqfs2tar (squashfs-tools-ng package)"
    echo "  - docker"
    exit 1
}

if [[ $# -lt 1 ]]; then
    usage
fi

ISO_PATH="$1"
IMAGE_TAG="${2:-vyos-gateway:test}"
WORK_DIR="${TMPDIR:-/tmp}/vyos-container-$$"

if [[ ! -f "${ISO_PATH}" ]]; then
    echo "ERROR: ISO file not found: ${ISO_PATH}"
    exit 1
fi

if [[ ! -f "${DOCKERFILE}" ]]; then
    echo "ERROR: Dockerfile not found: ${DOCKERFILE}"
    exit 1
fi

for cmd in 7z sqfs2tar docker; do
    if ! command -v "${cmd}" &>/dev/null; then
        echo "ERROR: Required command not found: ${cmd}"
        exit 1
    fi
done

cleanup() {
    rm -rf "${WORK_DIR}"
}
trap cleanup EXIT

mkdir -p "${WORK_DIR}"

# Check for cached rootfs.tar
CACHED_ROOTFS=""
if [[ -n "${CACHE_DIR}" ]]; then
    # Use first 12 chars of ISO sha256 as cache key
    ISO_HASH=$(sha256sum "${ISO_PATH}" | cut -d' ' -f1 | head -c 12)
    CACHED_ROOTFS="${CACHE_DIR}/rootfs-${ISO_HASH}.tar"

    if [[ -f "${CACHED_ROOTFS}" ]]; then
        echo "Using cached rootfs.tar: ${CACHED_ROOTFS}"
        ROOTFS_TAR="${CACHED_ROOTFS}"
    fi
fi

# Extract and convert if not cached
if [[ -z "${ROOTFS_TAR:-}" ]]; then
    echo "Extracting squashfs from ISO..."
    7z x -o"${WORK_DIR}" "${ISO_PATH}" "live/filesystem.squashfs" -y >/dev/null

    SQUASHFS="${WORK_DIR}/live/filesystem.squashfs"
    if [[ ! -f "${SQUASHFS}" ]]; then
        echo "ERROR: filesystem.squashfs not found in ISO"
        echo "Contents of ${WORK_DIR}:"
        find "${WORK_DIR}" -type f
        exit 1
    fi

    echo "Converting squashfs to rootfs.tar..."
    ROOTFS_TAR="${WORK_DIR}/rootfs.tar"
    sqfs2tar "${SQUASHFS}" > "${ROOTFS_TAR}"
    echo "rootfs.tar size: $(ls -lh "${ROOTFS_TAR}" | awk '{print $5}')"

    # Cache the rootfs.tar for future use
    if [[ -n "${CACHED_ROOTFS}" ]]; then
        echo "Caching rootfs.tar to: ${CACHED_ROOTFS}"
        cp "${ROOTFS_TAR}" "${CACHED_ROOTFS}"
    fi
fi

echo "Building container image: ${IMAGE_TAG}..."
BUILD_CONTEXT="${WORK_DIR}/build"
mkdir -p "${BUILD_CONTEXT}"
cp "${ROOTFS_TAR}" "${BUILD_CONTEXT}/rootfs.tar"
cp "${DOCKERFILE}" "${BUILD_CONTEXT}/Dockerfile"

docker build -t "${IMAGE_TAG}" -f "${BUILD_CONTEXT}/Dockerfile" "${BUILD_CONTEXT}"

echo "Container image built successfully: ${IMAGE_TAG}"
docker images "${IMAGE_TAG%%:*}" --format "table {{.Repository}}\t{{.Tag}}\t{{.Size}}"
