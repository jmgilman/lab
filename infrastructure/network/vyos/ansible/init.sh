#!/bin/bash
#
# init.sh - Initialize local environment for VyOS ansible management
#
# This script extracts the VyOS management SSH key from SOPS-encrypted
# storage and places it in ~/.ssh for use by ansible.
#
# Usage:
#   ./init.sh
#
# Requirements:
#   - sops (brew install sops)
#   - Access to SOPS decryption keys (age or GPG)
#

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SSH_SOPS_FILE="${SCRIPT_DIR}/../ssh.sops.yaml"
SSH_KEY_PATH="${HOME}/.ssh/vyos-gateway"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

error() {
    echo -e "${RED}[ERROR]${NC} $1"
    exit 1
}

# Check if sops is installed
if ! command -v sops &>/dev/null; then
    error "sops is not installed. Install with: brew install sops"
fi

# Check if SSH key file exists
if [[ ! -f "${SSH_SOPS_FILE}" ]]; then
    error "SSH credentials file not found: ${SSH_SOPS_FILE}"
fi

# Check if key already exists
if [[ -f "${SSH_KEY_PATH}" ]]; then
    warn "SSH key already exists at ${SSH_KEY_PATH}"
    read -p "Overwrite? [y/N] " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        info "Skipping SSH key extraction"
        exit 0
    fi
fi

# Extract private key from SOPS file
info "Decrypting SSH credentials..."
PRIVATE_KEY=$(sops -d --extract '["private_key"]' "${SSH_SOPS_FILE}" 2>/dev/null)

if [[ -z "${PRIVATE_KEY}" ]]; then
    error "Failed to decrypt SSH credentials"
fi

# Ensure ~/.ssh directory exists with correct permissions
mkdir -p "${HOME}/.ssh"
chmod 700 "${HOME}/.ssh"

# Write private key with correct permissions
info "Writing SSH key to ${SSH_KEY_PATH}..."
echo "${PRIVATE_KEY}" > "${SSH_KEY_PATH}"
chmod 600 "${SSH_KEY_PATH}"

# Also extract and write public key for convenience
info "Writing public key to ${SSH_KEY_PATH}.pub..."
PUBLIC_KEY=$(sops -d --extract '["public_key"]' "${SSH_SOPS_FILE}" 2>/dev/null)
echo "${PUBLIC_KEY}" > "${SSH_KEY_PATH}.pub"
chmod 644 "${SSH_KEY_PATH}.pub"

info "SSH key initialized successfully!"
echo ""
echo "Key location: ${SSH_KEY_PATH}"
echo ""
echo "You can now run ansible playbooks:"
echo "  cd ${SCRIPT_DIR}"
echo "  ansible-playbook playbooks/deploy.yml -i inventory/hosts.yml"
