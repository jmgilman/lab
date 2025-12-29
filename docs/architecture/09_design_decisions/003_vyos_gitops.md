# ADR 003: VyOS Configuration Management

**Status**: Accepted
**Date**: 2025-12-19

## Context

The VyOS gateway (VP6630) is a critical infrastructure component providing inter-VLAN routing, BGP peering, firewall rules, and DHCP. Currently, configuration is applied manually via SSH, which:

1. Creates **drift risk** — Undocumented changes accumulate
2. Increases **bus factor** — Only one person knows the config
3. Violates **GitOps principles** — Config is not in Git

We need a solution that brings VyOS under the same GitOps discipline as the rest of the infrastructure.

## Options

### Option A: Git Repo + Manual Apply
Store configuration in Git; SSH in and run `configure` / `load` manually when changes merge.

* **Pros**: Simple, no additional tooling
* **Cons**: Still requires manual intervention; easy to forget or fat-finger

### Option B: Ansible with vyos.vyos Collection
Use Ansible playbooks to declaratively manage VyOS configuration, triggered by CI/CD.

* **Pros**: Idempotent, testable, supports rollback via `commit-confirm`
* **Cons**: Requires network access from CI runner to VyOS

### Option C: VyOS REST API + Terraform
VyOS exposes an HTTPS configuration API; use a Terraform provider.

* **Pros**: Declarative, familiar tooling
* **Cons**: Less mature ecosystem; API requires HTTPS setup on VyOS

## Decision

**Use Option B: Ansible via GitHub Actions with Tailscale for secure access.**

## Implementation

### Architecture

VyOS configuration lives within the existing monorepo alongside all other infrastructure:

```
┌─────────────────────────────────────────────────────────────────────┐
│                          GitHub                                     │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │  Repository: lab (monorepo)                                 │    │
│  │  ├── docs/architecture/         (this documentation)       │    │
│  │  ├── clusters/                  (Argo CD apps)             │    │
│  │  ├── infrastructure/                                        │    │
│  │  │   └── vyos/                                              │    │
│  │  │       ├── ansible/                                       │    │
│  │  │       │   ├── playbooks/deploy.yml                       │    │
│  │  │       │   └── inventory/hosts.yml                        │    │
│  │  │       └── configs/vyos.conf                              │    │
│  │  └── .github/workflows/                                     │    │
│  │       ├── vyos-validate.yml   (PR: syntax checks)          │    │
│  │       └── vyos-deploy.yml     (merge: apply config)        │    │
│  └─────────────────────────────────────────────────────────────┘    │
└───────────────────────────────┬─────────────────────────────────────┘
                                │
                    ┌───────────▼───────────┐
                    │   GitHub Actions      │
                    │   (Runner)            │
                    └───────────┬───────────┘
                                │ Tailscale
                    ┌───────────▼───────────┐
                    │   VP6630 (VyOS)       │
                    │   Tailscale client    │
                    └───────────────────────┘
```

### Workflow: PR Validation

```yaml
# .github/workflows/vyos-validate.yml
name: Validate VyOS Config
on:
  pull_request:
    paths: ['infrastructure/vyos/**']
jobs:
  validate:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Lint Ansible
        run: ansible-lint infrastructure/vyos/ansible/
      - name: Syntax check playbook
        run: ansible-playbook infrastructure/vyos/ansible/playbooks/deploy.yml --syntax-check
      - name: Validate VyOS config syntax
        run: |
          # Use vyos container for config validation
          docker run --rm -v $PWD/infrastructure/network/vyos/configs:/config vyos/vyos:current \
            /opt/vyatta/sbin/vyatta-config-validator /config/gateway.conf
```

### Workflow: Deploy on Merge

```yaml
# .github/workflows/vyos-deploy.yml
name: Deploy VyOS Config
on:
  push:
    branches: [main]
    paths: ['infrastructure/vyos/**']
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Setup Tailscale
        uses: tailscale/github-action@v2
        with:
          oauth-client-id: ${{ secrets.TS_OAUTH_CLIENT_ID }}
          oauth-secret: ${{ secrets.TS_OAUTH_SECRET }}
          tags: tag:ci

      - name: Run Ansible
        env:
          ANSIBLE_HOST_KEY_CHECKING: false
        run: |
          ansible-playbook infrastructure/vyos/ansible/playbooks/deploy.yml \
            -i infrastructure/vyos/ansible/inventory/hosts.yml \
            --extra-vars "commit_confirm_timeout=5"
```

### Ansible Playbook (Simplified)

```yaml
# infrastructure/vyos/ansible/playbooks/deploy.yml
- name: Deploy VyOS Configuration
  hosts: vyos_gateway
  gather_facts: no
  tasks:
    - name: Load configuration with rollback protection
      vyos.vyos.vyos_config:
        src: "{{ playbook_dir }}/../../configs/vyos.conf"
        save: yes
        backup: yes
      vars:
        ansible_command_timeout: 60

    - name: Confirm commit (prevents auto-rollback)
      vyos.vyos.vyos_command:
        commands:
          - confirm
```

### Rollback Safety

VyOS `commit-confirm` provides automatic rollback:

1. Config is applied with a timeout (e.g., 5 minutes)
2. If `confirm` is not received, VyOS reverts to previous config
3. Protects against configs that break network access

### Runner Options

| Option | Pros | Cons |
|:---|:---|:---|
| **GitHub-hosted + Tailscale** | No infra to maintain | Tailscale client setup in CI |
| **Self-hosted runner (in-lab)** | Direct network access | Requires runner maintenance |

**Recommendation**: Start with GitHub-hosted + Tailscale for simplicity; migrate to self-hosted if Tailscale latency becomes problematic.

## Rationale

1. **GitOps Alignment**: Config changes flow through PR → Review → Merge → Deploy
2. **Safety**: `commit-confirm` prevents lockouts; PR validation catches syntax errors
3. **Auditability**: Full Git history of all configuration changes
4. **Existing Infrastructure**: Leverages existing Tailscale network

## Integration Testing

### Containerlab-Based Validation

To validate configuration changes before they reach production, we use [Containerlab](https://containerlab.dev/) to run integration tests on pull requests.

#### How It Works

1. **Container Image**: Uses the official VyOS container image from Docker Hub (`vyos/vyos:current`)

2. **Topology Simulation**: Containerlab deploys a test topology with:
   - VyOS gateway container
   - Simulated network clients for WAN, MGMT, and Platform networks

3. **Test Suite**: pytest with scrapli validates:
   - Firewall groups and rules
   - Interface configuration and addresses
   - DHCP, DNS, and BGP configuration
   - NAT/masquerade rules
   - Static routes and system settings

#### Test Files

```
infrastructure/network/vyos/
└── tests/
    ├── topology.clab.yml        # Containerlab topology
    ├── conftest.py              # pytest fixtures
    ├── test_gateway.py          # Test suite
    ├── render-config-boot.sh    # Generate config.boot with injected SSH key
    └── requirements.txt         # Python dependencies
```

#### Interface Mapping

The test environment uses simplified interface mapping:

| Production | Test | Network |
|:-----------|:-----|:--------|
| eth4 | eth1 | WAN |
| eth5.10 | eth2 | MGMT (VLAN 10) |
| eth5.30 | eth3 | Platform (VLAN 30) |

#### CI Integration

Integration tests run automatically on PRs modifying `infrastructure/network/vyos/**`:

1. `render-config-boot.sh` generates test config with SSH key injection
2. `integration-test` job deploys topology and runs pytest suite
3. Tests must pass before merge

## Consequences

- VyOS must run Tailscale client (or self-hosted runner needs lab network access)
- Secrets (Tailscale OAuth, SSH keys) managed in GitHub Secrets
- Initial effort to structure Ansible playbooks and test workflow
- Integration tests add build time (~5-8 minutes) but catch issues before production
