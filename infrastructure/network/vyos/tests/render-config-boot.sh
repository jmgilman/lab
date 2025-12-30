#!/bin/bash
# Render config.boot for containerlab testing
#
# Takes gateway.conf as the base configuration and:
# 1. Remaps production interfaces to test interfaces (Containerlab reserves eth0)
# 2. Injects an SSH public key for test authentication
# 3. Adjusts network addresses for the isolated test environment
#
# Interface Mapping (production -> test):
#   eth0 -> eth2 (WAN)
#   eth1 -> eth3 (Trunk)
#
# Usage: render-config-boot.sh <ssh_public_key>

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="${SCRIPT_DIR}/.."
CONFIG_FILE="${REPO_ROOT}/configs/gateway.conf"
OUTPUT_FILE="${SCRIPT_DIR}/config.boot"

usage() {
    echo "Usage: $0 <ssh_public_key>"
    echo ""
    echo "Example:"
    echo "  $0 \"ssh-ed25519 AAAA... comment\""
    exit 1
}

if [[ $# -ne 1 ]]; then
    usage
fi

SSH_PUBLIC_KEY="$1"
SSH_KEY_TYPE=$(echo "${SSH_PUBLIC_KEY}" | awk '{print $1}')
SSH_KEY_BODY=$(echo "${SSH_PUBLIC_KEY}" | awk '{print $2}')

if [[ -z "${SSH_KEY_TYPE}" ]] || [[ -z "${SSH_KEY_BODY}" ]]; then
    echo "ERROR: Could not parse SSH public key"
    echo "Expected format: 'type key [comment]'"
    exit 1
fi

if [[ ! -f "${CONFIG_FILE}" ]]; then
    echo "ERROR: Config file not found: ${CONFIG_FILE}"
    exit 1
fi

# Start with the base gateway.conf
cp "${CONFIG_FILE}" "${OUTPUT_FILE}"

# Remap interfaces for test environment (Containerlab reserves eth0 for management)
# Production: eth0 (WAN), eth1 (Trunk)
# Test: eth2 (WAN), eth3 (Trunk)
sed -i.bak -e 's/eth0/eth2/g' -e 's/eth1/eth3/g' "${OUTPUT_FILE}"
rm -f "${OUTPUT_FILE}.bak"

# Adjust WAN IP for test environment (192.168.0.0/24 instead of 10.0.0.0/30)
# This allows the test topology to use a simpler addressing scheme
sed -i.bak -e 's|10\.0\.0\.2/30|192.168.0.2/24|g' \
           -e 's|next-hop 10\.0\.0\.1|next-hop 192.168.0.1|g' \
           -e 's|192\.168\.1\.0/24|192.168.0.0/24|g' "${OUTPUT_FILE}"
rm -f "${OUTPUT_FILE}.bak"

# Inject SSH key into the system login section
# Find the closing brace of the system block and insert login config before it
# Use temp file approach for portability (macOS vs GNU sed)
TEMP_FILE=$(mktemp)
sed '/^system {$/,/^}$/{
    /^}$/i\
    login {\
        user vyos {\
            authentication {\
                public-keys test {\
                    key "'"${SSH_KEY_BODY}"'"\
                    type '"${SSH_KEY_TYPE}"'\
                }\
            }\
        }\
    }
}' "${OUTPUT_FILE}" > "${TEMP_FILE}"
mv "${TEMP_FILE}" "${OUTPUT_FILE}"

# Fix SELinux context if applicable (for container environments)
if command -v getenforce >/dev/null 2>&1 && command -v chcon >/dev/null 2>&1; then
    if [[ "$(getenforce)" == "Enforcing" ]]; then
        if [[ "${EUID}" -ne 0 ]] && command -v sudo >/dev/null 2>&1; then
            sudo chcon -t container_file_t "${OUTPUT_FILE}" || true
        else
            chcon -t container_file_t "${OUTPUT_FILE}" || true
        fi
    fi
fi

echo "Wrote ${OUTPUT_FILE}"
