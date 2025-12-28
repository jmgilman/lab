#!/bin/bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="${SCRIPT_DIR}/.."
TEMPLATE_FILE="${REPO_ROOT}/templates/gateway.toml"
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

if [[ ! -f "${TEMPLATE_FILE}" ]]; then
    echo "ERROR: Template file not found: ${TEMPLATE_FILE}"
    exit 1
fi

sed -n "/^default_config = '''$/,/^'''$/p" "${TEMPLATE_FILE}" \
    | sed '1d;$d' \
    | sed -e "s|%%SSH_KEY_TYPE%%|${SSH_KEY_TYPE}|g" \
          -e "s|%%SSH_PUBLIC_KEY%%|${SSH_KEY_BODY}|g" \
    > "${OUTPUT_FILE}"

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
