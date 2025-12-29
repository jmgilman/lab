#!/bin/bash
#
# talos-embed-config.sh - Embed Talos machine configuration into an ISO
#
# Usage: talos-embed-config.sh <iso-path> <node-hostname>
#
# This is a transform hook for the labctl image sync pipeline. It:
# 1. Generates machine configuration using talhelper from talconfig.yaml
# 2. Creates a new ISO with embedded config using the Talos imager
# 3. Replaces the downloaded ISO with the new embedded version
#
# Note: The downloaded base ISO is discarded. The imager creates a fresh ISO
# with the machine configuration embedded. This ensures the ISO version matches
# the Talos version specified in talconfig.yaml.
#
# Exit codes:
#   0 - Success
#   1 - Error occurred
#
# Environment variables:
#   TALOS_VERSION - Talos version to use for imager (default: from talconfig.yaml)

set -euo pipefail

if [[ $# -lt 2 ]]; then
    echo "Usage: $0 <iso-path> <node-hostname>" >&2
    exit 1
fi

ISO_PATH="$1"
NODE_HOSTNAME="$2"

# Derive paths from script location
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
TALOS_DIR="${REPO_ROOT}/infrastructure/compute/talos"

# Temporary directory for work files
WORK_DIR=$(mktemp -d)

echo "Talos ISO Configuration Embedding"
echo "  ISO: ${ISO_PATH}"
echo "  Node: ${NODE_HOSTNAME}"
echo "  Work dir: ${WORK_DIR}"
echo ""

cleanup() {
    local exit_code=$?
    if [[ -d "${WORK_DIR}" ]]; then
        rm -rf "${WORK_DIR}"
    fi
    exit $exit_code
}
trap cleanup EXIT

# Check for required dependencies
check_deps() {
    local missing=()

    command -v docker &>/dev/null || missing+=("docker")
    command -v talhelper &>/dev/null || missing+=("talhelper")
    command -v sops &>/dev/null || missing+=("sops")

    if [[ ${#missing[@]} -gt 0 ]]; then
        echo "ERROR: Missing dependencies: ${missing[*]}" >&2
        echo "Install with:" >&2
        echo "  brew install talhelper sops" >&2
        echo "  Docker must be installed and running" >&2
        exit 1
    fi

    # Check if docker is running
    if ! docker info &>/dev/null; then
        echo "ERROR: Docker is not running" >&2
        exit 1
    fi
}

# Extract Talos version from talconfig.yaml
get_talos_version() {
    if [[ -n "${TALOS_VERSION:-}" ]]; then
        echo "${TALOS_VERSION}"
        return
    fi

    local version
    version=$(grep -E "^talosVersion:" "${TALOS_DIR}/talconfig.yaml" | awk '{print $2}' | tr -d '"' | tr -d "'")
    if [[ -z "${version}" ]]; then
        echo "ERROR: Could not determine Talos version from talconfig.yaml" >&2
        exit 1
    fi
    echo "${version}"
}

# Generate machine configuration using talhelper
generate_config() {
    echo "Generating machine configuration..."

    cd "${TALOS_DIR}"

    # Generate configs to work directory
    talhelper genconfig --out-dir "${WORK_DIR}/clusterconfig"

    # Find the config file for the specified node
    local cluster_name
    cluster_name=$(grep -E "^clusterName:" "${TALOS_DIR}/talconfig.yaml" | awk '{print $2}' | tr -d '"' | tr -d "'")

    local config_file="${WORK_DIR}/clusterconfig/${cluster_name}-${NODE_HOSTNAME}.yaml"

    if [[ ! -f "${config_file}" ]]; then
        echo "ERROR: Config file not found: ${config_file}" >&2
        echo "Available configs:" >&2
        ls -la "${WORK_DIR}/clusterconfig/" >&2
        exit 1
    fi

    echo "  Generated config: ${config_file}"
    echo "${config_file}"
}

# Embed configuration into ISO using Talos imager
embed_config() {
    local config_file="$1"
    local talos_version
    talos_version=$(get_talos_version)

    echo "Creating ISO with embedded configuration..."
    echo "  Talos version: ${talos_version}"

    # Create output directory for imager
    mkdir -p "${WORK_DIR}/out"

    # Copy config to a location the imager can access
    cp "${config_file}" "${WORK_DIR}/machine.yaml"

    # Run the imager to create a new ISO with embedded config
    # The imager creates a fresh ISO from scratch - it doesn't modify the downloaded ISO
    docker run --rm \
        -v "${WORK_DIR}:/work" \
        "ghcr.io/siderolabs/imager:${talos_version}" \
        iso \
        --arch amd64 \
        --output-path /work/out \
        --embedded-config-path /work/machine.yaml

    # Find the generated ISO
    local output_iso="${WORK_DIR}/out/metal-amd64.iso"

    if [[ ! -f "${output_iso}" ]]; then
        echo "ERROR: Output ISO not found: ${output_iso}" >&2
        ls -la "${WORK_DIR}/out/" >&2
        exit 1
    fi

    # Replace the downloaded ISO with the newly created embedded version
    mv "${output_iso}" "${ISO_PATH}"

    echo "  Embedded ISO: ${ISO_PATH}"
}

# Main execution
main() {
    check_deps

    local config_file
    config_file=$(generate_config)

    embed_config "${config_file}"

    echo ""
    echo "Successfully embedded configuration for ${NODE_HOSTNAME} into ISO"
}

main
