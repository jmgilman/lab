#!/bin/bash
# Render config.boot for containerlab testing
#
# Takes gateway.conf as the base configuration and injects an SSH public key
# for test authentication.
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

# Inject SSH key into the system login section
# Find the closing brace of the system block and insert login config before it
sed -i '' '/^system {$/,/^}$/{
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
}' "${OUTPUT_FILE}"

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
