#!/bin/bash
#
# vyos-test.sh - Run VyOS integration tests against an ISO
#
# Usage: vyos-test.sh <iso-path>
#
# This script:
# 1. Converts the ISO to a container image
# 2. Deploys a containerlab topology
# 3. Runs pytest integration tests
# 4. Cleans up resources
#
# Exit codes:
#   0 - All tests passed
#   1 - Tests failed or error occurred

set -euo pipefail

if [[ $# -lt 1 ]]; then
    echo "Usage: $0 <iso-path>" >&2
    exit 1
fi

ISO_PATH="$1"

# Derive paths from script location
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
VYOS_DIR="${REPO_ROOT}/infrastructure/network/vyos"
TEST_DIR="${VYOS_DIR}/tests"

# Use unique identifiers to avoid conflicts in parallel runs
RUN_ID="$$"
IMAGE_TAG="vyos-gateway:hook-test-${RUN_ID}"
SSH_KEY="/tmp/vyos-test-key-${RUN_ID}"
TOPOLOGY_BACKUP="${TEST_DIR}/topology.clab.yml.bak-${RUN_ID}"

echo "VyOS Integration Test"
echo "  ISO: ${ISO_PATH}"
echo "  Run ID: ${RUN_ID}"
echo ""

cleanup() {
    local exit_code=$?
    echo ""
    echo "Cleaning up..."

    # Destroy containerlab topology
    if [[ -f "${TEST_DIR}/topology.clab.yml" ]]; then
        cd "${TEST_DIR}" && sudo containerlab destroy -t topology.clab.yml --cleanup 2>/dev/null || true
    fi

    # Restore original topology if we modified it
    if [[ -f "${TOPOLOGY_BACKUP}" ]]; then
        mv "${TOPOLOGY_BACKUP}" "${TEST_DIR}/topology.clab.yml"
    fi

    # Remove test SSH key
    rm -f "${SSH_KEY}" "${SSH_KEY}.pub" 2>/dev/null || true

    # Remove test container image
    docker rmi "${IMAGE_TAG}" 2>/dev/null || true

    exit $exit_code
}
trap cleanup EXIT

# Check for required dependencies
check_deps() {
    local missing=()

    command -v 7z &>/dev/null || missing+=("p7zip-full")
    command -v sqfs2tar &>/dev/null || missing+=("squashfs-tools-ng")
    command -v containerlab &>/dev/null || missing+=("containerlab")
    command -v pytest &>/dev/null || missing+=("pytest (pip)")

    if [[ ${#missing[@]} -gt 0 ]]; then
        echo "ERROR: Missing dependencies: ${missing[*]}" >&2
        echo "Install with:" >&2
        echo "  apt-get install p7zip-full squashfs-tools-ng" >&2
        echo "  bash -c \"\$(curl -sL https://get.containerlab.dev)\"" >&2
        echo "  pip install -r ${TEST_DIR}/requirements.txt" >&2
        exit 1
    fi
}

# Convert ISO to container image
build_container() {
    echo "Building container image from ISO..."
    "${VYOS_DIR}/scripts/iso-to-container.sh" "${ISO_PATH}" "${IMAGE_TAG}"
    echo "  Container image: ${IMAGE_TAG}"
}

# Generate test configuration
generate_config() {
    echo "Generating test configuration..."

    # Create test SSH key
    ssh-keygen -t ed25519 -f "${SSH_KEY}" -N "" -C "vyos-hook-test-${RUN_ID}" -q

    # Render config.boot with test SSH key
    "${TEST_DIR}/render-config-boot.sh" "$(cat "${SSH_KEY}.pub")"

    echo "  SSH key: ${SSH_KEY}"
}

# Update topology to use our test image
update_topology() {
    echo "Updating topology for test run..."

    # Backup original topology
    cp "${TEST_DIR}/topology.clab.yml" "${TOPOLOGY_BACKUP}"

    # Update image reference
    sed -i "s|image: vyos-gateway:test|image: ${IMAGE_TAG}|g" "${TEST_DIR}/topology.clab.yml"
}

# Deploy containerlab topology
deploy_topology() {
    echo "Deploying containerlab topology..."
    cd "${TEST_DIR}"
    sudo containerlab deploy -t topology.clab.yml --reconfigure
}

# Wait for VyOS to be ready
wait_for_vyos() {
    local container="clab-vyos-gateway-test-gateway"

    echo "Waiting for VyOS to be ready..."

    # Wait for container to be running
    for i in {1..30}; do
        if docker ps --filter "name=${container}" --filter "status=running" | grep -q "${container}"; then
            echo "  Container is running"
            break
        fi
        echo "  Waiting for container... ($i/30)"
        sleep 2
    done

    # Load kernel modules
    echo "  Loading kernel modules..."
    sudo modprobe 8021q 2>/dev/null || true
    sudo docker exec "${container}" modprobe br_netfilter 2>/dev/null || true
    sudo docker exec "${container}" modprobe 8021q 2>/dev/null || true

    # Wait for container to become healthy
    # The Dockerfile healthcheck uses: systemctl is-system-running --quiet
    echo "  Waiting for container to become healthy..."
    for i in {1..60}; do
        health=$(docker inspect --format='{{.State.Health.Status}}' "${container}" 2>/dev/null || echo "unknown")
        if [[ "${health}" == "healthy" ]]; then
            echo "  Container is healthy"
            break
        fi
        if [[ $i -eq 60 ]]; then
            echo "  WARNING: Container did not become healthy within timeout"
        fi
        sleep 2
    done

    # Wait briefly for VyOS services to fully initialize after systemd reports ready
    echo "  Waiting for VyOS services to initialize..."
    sleep 5

    # Verify configuration loaded
    echo "  Verifying configuration..."
    docker exec "${container}" /opt/vyatta/bin/vyatta-op-cmd-wrapper show configuration commands | head -5

    # Check DHCP server
    echo "  Checking DHCP server..."
    docker exec "${container}" pgrep -a kea-dhcp4 || echo "  WARNING: kea-dhcp4 not running yet"
}

# Run integration tests
run_tests() {
    echo ""
    echo "Running integration tests..."
    cd "${TEST_DIR}"

    export VYOS_SSH_KEY="${SSH_KEY}"
    pytest -v --tb=short -x

    echo ""
    echo "All tests passed!"
}

# Main execution
main() {
    check_deps
    build_container
    generate_config
    update_topology
    deploy_topology
    wait_for_vyos
    run_tests
}

main
