# Appendix A: Repository Structure Reference

> **Status**: Authoritative Reference
> **Date**: 2025-12-19

This document defines the canonical directory structure for the lab monorepo. All code, configuration, and documentation must follow this structure.

---

## Table of Contents

1. [Complete Directory Tree](#complete-directory-tree)
2. [Top-Level Directory Overview](#top-level-directory-overview)
3. [clusters/ - Argo CD Managed Clusters](#clusters---argo-cd-managed-clusters)
4. [packages/ - Crossplane Configuration Packages](#packages---crossplane-configuration-packages)
5. [infrastructure/ - Pre-Kubernetes Configuration](#infrastructure---pre-kubernetes-configuration)
6. [bootstrap/ - Bootstrap and Recovery](#bootstrap---bootstrap-and-recovery)
7. [Management Summary](#management-summary)
8. [Naming Conventions](#naming-conventions)

---

## Complete Directory Tree

```
lab/
│
├── .github/
│   └── workflows/
│       ├── crossplane-build.yml          # Build Crossplane packages on tag
│       ├── vyos-validate.yml             # PR validation for VyOS config
│       └── vyos-deploy.yml               # Deploy VyOS config on merge
│
├── docs/
│   └── architecture/                     # arc42 documentation
│
├── infrastructure/                       # NOT Argo CD managed
│   │                                     # Pre-K8s / out-of-band configuration
│   │
│   ├── network/
│   │   └── vyos/
│   │       ├── configs/
│   │       │   └── gateway.conf              # Production VyOS config (loaded manually at install)
│   │       ├── ansible/
│   │       │   ├── playbooks/
│   │       │   │   └── deploy.yml            # Ansible playbook for config updates
│   │       │   └── inventory/
│   │       │       └── hosts.yml
│   │       ├── scripts/
│   │       │   └── iso-to-container.sh       # Convert ISO to container for testing
│   │       └── tests/
│   │           ├── topology.clab.yml         # Containerlab test topology
│   │           └── test_*.py                 # Integration tests
│   │
│   ├── compute/
│   │   └── talos/
│   │       │                             # talhelper-managed Talos configs
│   │       │                             # For platform cluster (bare-metal/PXE)
│   │       │
│   │       ├── talconfig.yaml            # talhelper configuration
│   │       ├── talsecret.sops.yaml       # Encrypted Talos secrets
│   │       ├── clusterconfig/            # Generated machine configs (gitignored)
│   │       │   └── .gitkeep
│   │       └── patches/                  # Machine config patches
│   │           ├── common.yaml
│   │           ├── controlplane.yaml
│   │           └── worker.yaml
│   │
│   └── provisioning/
│       └── tinkerbell/
│           │                             # Tinkerbell workflow definitions
│           │                             # (Templates, not instances — instances are XRs)
│           │
│           └── templates/
│               ├── harvester-install.yaml
│               └── talos-install.yaml
│
├── packages/                             # Crossplane Configuration Packages
│   │                                     # Built as OCI → ghcr.io/gilmanlab/xrp-*
│   │
│   ├── infrastructure/                   # Tag: infrastructure/vX.Y.Z
│   │   ├── crossplane.yaml
│   │   ├── apis/
│   │   │   ├── tenant-cluster/
│   │   │   │   ├── definition.yaml       # TenantCluster XRD
│   │   │   │   └── composition.yaml
│   │   │   ├── hardware/
│   │   │   │   ├── definition.yaml       # Hardware XRD (Tinkerbell)
│   │   │   │   └── composition.yaml
│   │   │   └── workflow/
│   │   │       ├── definition.yaml       # Workflow XRD (Tinkerbell)
│   │   │       └── composition.yaml
│   │   └── functions/
│   │       └── ...
│   │
│   └── platform/                         # Tag: platform/vX.Y.Z
│       ├── crossplane.yaml
│       ├── apis/
│       │   ├── core-services/
│       │   │   ├── definition.yaml       # CoreServices XRD (base layer)
│       │   │   └── composition.yaml
│       │   ├── platform-services/
│       │   │   ├── definition.yaml       # PlatformServices XRD (shared services)
│       │   │   └── composition.yaml
│       │   ├── application/
│       │   │   ├── definition.yaml       # Application XRD
│       │   │   └── composition.yaml
│       │   └── database/
│       │       ├── definition.yaml       # Database XRD (CloudNativePG)
│       │       └── composition.yaml
│       └── functions/
│           └── ...
│
├── clusters/                             # ALL Clusters - Argo CD managed
│   │                                     # Discovered by ApplicationSet: clusters/*/
│   │
│   ├── platform/                         # Platform Cluster
│   │   │                                 # Special: No cluster.yaml (bare-metal bootstrap)
│   │   │
│   │   ├── core.yaml                     # CoreServices XR (Crossplane, CAPI, cert-manager, etc.)
│   │   ├── platform.yaml                 # PlatformServices XR (Zitadel, OpenBAO, etc.)
│   │   └── apps/
│   │       ├── tinkerbell/
│   │       │   ├── hardware/
│   │       │   │   ├── ms02-node1.yaml   # Hardware XR
│   │       │   │   ├── ms02-node2.yaml
│   │       │   │   ├── ms02-node3.yaml
│   │       │   │   └── um760.yaml
│   │       │   └── workflows/
│   │       │       ├── harvester.yaml    # Workflow XR
│   │       │       └── talos.yaml
│   │       ├── observability/
│   │       │   ├── prometheus.yaml       # Application XR
│   │       │   ├── grafana.yaml
│   │       │   └── loki.yaml
│   │       └── capi/
│   │           ├── providers.yaml        # CAPI provider configs
│   │           └── harvester-config.yaml
│   │
│   ├── harvester/                        # Harvester HCI Cluster
│   │   │                                 # Raw Harvester CRDs (NOT Crossplane XRs)
│   │   │                                 # Registered with Argo CD after Harvester boots
│   │   │
│   │   ├── config/                       # Harvester configuration
│   │   │   ├── networks/
│   │   │   │   ├── mgmt.yaml             # VLAN 10 - Management
│   │   │   │   ├── platform.yaml         # VLAN 30 - Platform cluster
│   │   │   │   ├── cluster.yaml          # VLAN 40 - Tenant clusters
│   │   │   │   └── storage.yaml          # VLAN 60 - Storage replication
│   │   │   └── images/
│   │   │       └── talos-1.9.yaml        # Talos VM image template
│   │   │
│   │   └── vms/                          # Virtual Machines
│   │       ├── platform/                 # Platform cluster VMs (bootstrap)
│   │       │   ├── cp-2.yaml             # Platform control plane node 2
│   │       │   └── cp-3.yaml             # Platform control plane node 3
│   │       └── standalone/               # Non-container workloads
│   │           └── .gitkeep              # (windows-gaming.yaml, truenas.yaml, etc.)
│   │
│   ├── media/                            # Tenant Cluster
│   │   ├── cluster.yaml                  # TenantCluster XR
│   │   ├── core.yaml                     # CoreServices XR
│   │   └── apps/
│   │       ├── plex/
│   │       │   └── plex.yaml             # Application XR
│   │       ├── jellyfin/
│   │       │   └── jellyfin.yaml
│   │       └── arr-stack/
│   │           └── arr-stack.yaml
│   │
│   ├── dev/                              # Tenant Cluster
│   │   ├── cluster.yaml
│   │   ├── core.yaml
│   │   └── apps/
│   │       ├── gitea/
│   │       │   └── gitea.yaml
│   │       └── runners/
│   │           └── runners.yaml
│   │
│   └── prod/                             # Tenant Cluster
│       ├── cluster.yaml
│       ├── core.yaml
│       └── apps/
│           └── ...
│
└── bootstrap/
    │
    ├── seed/                             # One-time bootstrap manifests (raw K8s, NOT XRs)
    │   │                                 # Deployed via temporary Argo CD Application
    │   │
    │   ├── tinkerbell/                   # Tinkerbell deployment (Helm or raw)
    │   │   ├── namespace.yaml
    │   │   └── release.yaml              # HelmRelease or raw manifests
    │   │
    │   ├── hardware/                     # Hardware definitions for bootstrap
    │   │   ├── vp6630.yaml               # Raw Tinkerbell Hardware CRD (VyOS router)
    │   │   └── um760.yaml                # Raw Tinkerbell Hardware CRD (platform node)
    │   │
    │   ├── workflows/                    # Provisioning workflows for bootstrap
    │   │   ├── vp6630-vyos.yaml          # Raw Tinkerbell Workflow CRD (VyOS install)
    │   │   └── um760-talos.yaml          # Raw Tinkerbell Workflow CRD (Talos install)
    │   │
    │   └── config-server/                # HTTP server for Talos configs
    │       └── nginx.yaml                # Serves talhelper-generated configs
    │
    ├── genesis/                          # Runbooks and scripts
    │   ├── README.md                     # Overview and prerequisites
    │   └── scripts/
    │       └── install-argocd.sh         # Helm install Argo CD
    │
    └── recovery/
        ├── etcd-restore.md
        ├── platform-rebuild.md
        └── scripts/
            └── restore-etcd.sh
```

---

## Top-Level Directory Overview

| Directory | Purpose | Managed By | Git Tracked |
|:----------|:--------|:-----------|:------------|
| `.github/` | CI/CD workflows for building packages and deploying VyOS | GitHub Actions | Yes |
| `docs/` | arc42 architecture documentation | Manual | Yes |
| `infrastructure/` | Pre-Kubernetes and out-of-band configuration | Manual, Ansible | Yes |
| `packages/` | Crossplane Configuration Packages (OCI artifacts) | Built by CI/CD | Yes (source) |
| `clusters/` | Cluster definitions and application deployments | Argo CD | Yes |
| `bootstrap/` | Bootstrap manifests, runbooks, and recovery procedures | Manual (one-time) | Yes |

---

## clusters/ - Argo CD Managed Clusters

The `clusters/` directory contains all Kubernetes cluster configurations. Each subdirectory represents a cluster and is automatically discovered by Argo CD ApplicationSets.

### Directory Pattern

```
clusters/
├── <cluster-name>/
│   ├── cluster.yaml      # TenantCluster XR (optional, not for platform)
│   ├── core.yaml         # CoreServices XR (optional)
│   ├── platform.yaml     # PlatformServices XR (optional, platform only)
│   ├── config/           # Raw cluster configuration (Harvester only)
│   ├── vms/              # VirtualMachine CRDs (Harvester only)
│   └── apps/             # Application deployments
│       └── <app-name>/
│           └── *.yaml
```

### Cluster Types

| Cluster Type | cluster.yaml | core.yaml | platform.yaml | config/ | vms/ | apps/ |
|:-------------|:------------:|:---------:|:-------------:|:-------:|:----:|:-----:|
| **Platform** | N/A | Required | Required | N/A | N/A | Required |
| **Harvester** | N/A | N/A | N/A | Required | Required | N/A |
| **Tenant** | Required | Required | N/A | N/A | N/A | Required |

### Platform Cluster (`clusters/platform/`)

The platform cluster is the control plane for the entire lab infrastructure. It hosts Crossplane, Argo CD, CAPI, and shared services.

**Key Characteristics:**
- No `cluster.yaml` - bootstrapped via bare-metal PXE (Tinkerbell + talhelper)
- Hybrid deployment: UM760 (bare-metal) + CP-2/CP-3 (Harvester VMs)
- Contains XR Claims for CoreServices and PlatformServices
- Applications deployed via `apps/` subdirectories

**File Structure:**

```
clusters/platform/
├── core.yaml                           # CoreServices XR
│                                       # - Crossplane
│                                       # - CAPI (Cluster API)
│                                       # - cert-manager
│                                       # - external-dns
│                                       # - Istio
│
├── platform.yaml                       # PlatformServices XR
│                                       # - Zitadel (identity)
│                                       # - OpenBAO (secrets)
│                                       # - Vault Secrets Operator
│
└── apps/
    ├── tinkerbell/
    │   ├── hardware/
    │   │   ├── ms02-node1.yaml         # Hardware XR for Harvester node 1
    │   │   ├── ms02-node2.yaml         # Hardware XR for Harvester node 2
    │   │   ├── ms02-node3.yaml         # Hardware XR for Harvester node 3
    │   │   └── um760.yaml              # Hardware XR for platform node 1
    │   └── workflows/
    │       ├── harvester.yaml          # Workflow XR for Harvester installation
    │       └── talos.yaml              # Workflow XR for Talos installation
    │
    ├── observability/
    │   ├── prometheus.yaml             # Application XR
    │   ├── grafana.yaml                # Application XR
    │   └── loki.yaml                   # Application XR
    │
    └── capi/
        ├── providers.yaml              # CAPI provider configurations
        └── harvester-config.yaml       # Harvester provider setup
```

**Why No `cluster.yaml`?**

The platform cluster cannot use the TenantCluster XR because that XR requires Crossplane and CAPI to already be running. The platform must be bootstrapped via PXE using Tinkerbell and talhelper. See [Appendix B: Bootstrap Procedure](B_bootstrap_procedure.md) for details.

### Harvester Cluster (`clusters/harvester/`)

Harvester is a hyperconverged infrastructure (HCI) cluster that provides VM hosting and storage for the lab. It uses native Harvester CRDs rather than Crossplane XRs.

**Key Characteristics:**
- No Crossplane abstractions - raw Harvester CRDs
- Provisioned via Tinkerbell PXE boot on MS-02 nodes
- Registered as a managed cluster in Argo CD
- Hosts platform cluster VMs (CP-2, CP-3) and standalone VMs

**File Structure:**

```
clusters/harvester/
├── config/
│   ├── networks/
│   │   ├── mgmt.yaml                   # VLAN 10 - Management network
│   │   ├── platform.yaml               # VLAN 30 - Platform cluster network
│   │   ├── cluster.yaml                # VLAN 40 - Tenant cluster network
│   │   └── storage.yaml                # VLAN 60 - Storage replication network
│   │
│   └── images/
│       └── talos-1.9.yaml              # Talos OS VM image template
│
└── vms/
    ├── platform/
    │   ├── cp-2.yaml                   # Platform control plane node 2
    │   └── cp-3.yaml                   # Platform control plane node 3
    │
    └── standalone/
        └── .gitkeep                    # Future: windows-gaming.yaml, truenas.yaml
```

**Network Configuration:**

| File | VLAN | Purpose | Subnet |
|:-----|:----:|:--------|:-------|
| `mgmt.yaml` | 10 | Management network | 10.10.10.0/24 |
| `platform.yaml` | 30 | Platform cluster nodes | 10.10.30.0/24 |
| `cluster.yaml` | 40 | Tenant cluster nodes | 10.10.40.0/24 |
| `storage.yaml` | 60 | Ceph replication traffic | 10.10.60.0/24 |

**VM Types:**

- **platform/** - Platform cluster VMs created during bootstrap (before CAPI is available)
- **standalone/** - Non-containerized workloads (gaming PCs, file servers, etc.)
- Tenant cluster VMs are NOT here - they are managed by CAPI via TenantCluster XR

### Tenant Clusters (`clusters/media/`, `clusters/dev/`, `clusters/prod/`)

Tenant clusters are application workload clusters provisioned entirely via Crossplane and CAPI.

**Key Characteristics:**
- Provisioned via TenantCluster XR Claim
- CAPI creates Harvester VMs automatically
- Automatically registered with Argo CD (via XR composition)
- Applications deployed via XR Claims

**File Structure:**

```
clusters/<tenant>/
├── cluster.yaml                        # TenantCluster XR
│                                       # Specifies: node count, resources, networking
│
├── core.yaml                           # CoreServices XR
│                                       # - Istio
│                                       # - cert-manager
│                                       # - external-dns
│
└── apps/
    └── <app-name>/
        └── <app-name>.yaml             # Application XR
```

**Example: Media Cluster**

```
clusters/media/
├── cluster.yaml                        # TenantCluster XR: 3 nodes, 32GB RAM each
├── core.yaml                           # CoreServices XR
└── apps/
    ├── plex/
    │   └── plex.yaml                   # Application XR
    ├── jellyfin/
    │   └── jellyfin.yaml               # Application XR
    └── arr-stack/
        └── arr-stack.yaml              # Application XR (Sonarr, Radarr, etc.)
```

### Cluster Discovery and Routing

Argo CD uses two ApplicationSets to manage clusters:

1. **cluster-definitions** - Syncs `*.yaml` files (cluster/core/platform XRs) to platform cluster
2. **cluster-apps** - Uses matrix generator to sync `apps/*` to respective clusters

See [ADR-003: GitOps Structure](../09_design_decisions/ADR-003-gitops-structure.md) for details.

---

## packages/ - Crossplane Configuration Packages

Crossplane Configuration Packages define the XRDs (Custom Resource Definitions) and Compositions that power the platform's declarative infrastructure.

### Package Types

| Package | OCI Image | Git Tag Pattern | Purpose |
|:--------|:----------|:----------------|:--------|
| `packages/infrastructure/` | `ghcr.io/gilmanlab/xrp-infrastructure` | `infrastructure/vX.Y.Z` | Infrastructure primitives (TenantCluster, Hardware, Workflow) |
| `packages/platform/` | `ghcr.io/gilmanlab/xrp-platform` | `platform/vX.Y.Z` | Platform services (CoreServices, PlatformServices, Application, Database) |

### Infrastructure Package

**Path:** `packages/infrastructure/`

Defines infrastructure-level abstractions for cluster and hardware provisioning.

**Structure:**

```
packages/infrastructure/
├── crossplane.yaml                     # Package metadata
├── apis/
│   ├── tenant-cluster/
│   │   ├── definition.yaml             # TenantCluster XRD
│   │   └── composition.yaml            # CAPI + Harvester composition
│   │
│   ├── hardware/
│   │   ├── definition.yaml             # Hardware XRD (Tinkerbell)
│   │   └── composition.yaml            # Tinkerbell Hardware CRD composition
│   │
│   └── workflow/
│       ├── definition.yaml             # Workflow XRD (Tinkerbell)
│       └── composition.yaml            # Tinkerbell Workflow CRD composition
│
└── functions/
    └── ...                             # Crossplane composition functions
```

**XRDs Defined:**

- **TenantCluster** - Provisions a complete Kubernetes cluster via CAPI on Harvester
- **Hardware** - Registers bare-metal hardware with Tinkerbell for PXE provisioning
- **Workflow** - Defines Tinkerbell workflows for OS installation (Talos, Harvester)

### Platform Package

**Path:** `packages/platform/`

Defines platform-level abstractions for Kubernetes workloads and services.

**Structure:**

```
packages/platform/
├── crossplane.yaml                     # Package metadata
├── apis/
│   ├── core-services/
│   │   ├── definition.yaml             # CoreServices XRD
│   │   └── composition.yaml            # Base cluster services
│   │
│   ├── platform-services/
│   │   ├── definition.yaml             # PlatformServices XRD
│   │   └── composition.yaml            # Shared platform services
│   │
│   ├── application/
│   │   ├── definition.yaml             # Application XRD
│   │   └── composition.yaml            # Standardized app deployment
│   │
│   └── database/
│       ├── definition.yaml             # Database XRD
│       └── composition.yaml            # CloudNativePG composition
│
└── functions/
    └── ...                             # Crossplane composition functions
```

**XRDs Defined:**

- **CoreServices** - Base cluster services (Crossplane, CAPI, cert-manager, Istio, etc.)
- **PlatformServices** - Shared services (Zitadel, OpenBAO, VSO)
- **Application** - Standardized application deployment (Helm + Istio ingress)
- **Database** - PostgreSQL database provisioning (CloudNativePG)

### Build and Release Process

Crossplane packages are built and published automatically by GitHub Actions:

1. Developer tags commit: `git tag infrastructure/v1.2.3`
2. GitHub Action `.github/workflows/crossplane-build.yml` triggers
3. Package is built using `crossplane xpkg build`
4. OCI artifact is pushed to `ghcr.io/gilmanlab/xrp-infrastructure:v1.2.3`
5. Argo CD can reference the new version in package installations

**Workflow Files:**

- `.github/workflows/crossplane-build.yml` - Builds and publishes packages on git tag push

---

## infrastructure/ - Pre-Kubernetes Configuration

The `infrastructure/` directory contains configuration for systems that exist outside or before Kubernetes. This is NOT managed by Argo CD.

### Structure

```
infrastructure/
├── network/
│   └── vyos/                           # VyOS router configuration
│       ├── configs/
│       │   └── gateway.conf            # VyOS declarative config
│       └── ansible/
│           ├── playbooks/
│           │   └── deploy.yml          # Ansible playbook for VyOS deployment
│           └── inventory/
│               └── hosts.yml           # VyOS host inventory
│
├── compute/
│   └── talos/                          # Talos configuration for platform cluster
│       ├── talconfig.yaml              # talhelper configuration file
│       ├── talsecret.sops.yaml         # SOPS-encrypted secrets
│       ├── clusterconfig/              # Generated machine configs (gitignored)
│       │   └── .gitkeep
│       └── patches/                    # Machine config patches
│           ├── common.yaml             # Applied to all nodes
│           ├── controlplane.yaml       # Applied to control plane nodes
│           └── worker.yaml             # Applied to worker nodes
│
└── provisioning/
    └── tinkerbell/                     # Tinkerbell workflow templates
        └── templates/
            ├── harvester-install.yaml  # Harvester OS installation workflow
            └── talos-install.yaml      # Talos OS installation workflow
```

### Network (`infrastructure/network/vyos/`)

VyOS provides the lab's core networking: routing, firewall, DHCP, and VPN.

**Bootstrap (Manual Install):**
- VyOS is installed manually from the official Stream ISO during genesis bootstrap
- Configuration is loaded from `infrastructure/network/vyos/configs/gateway.conf` via USB
- This is a one-time manual step; VyOS must be operational before the platform cluster can bootstrap
- See [Appendix B: Bootstrap Procedure](B_bootstrap_procedure.md) for step-by-step instructions

**Configuration (`infrastructure/network/vyos/configs/gateway.conf`):**
- Declarative VyOS configuration file (curly-brace format)
- Includes: VLANs, DHCP relay, BGP peering config, firewall rules, NAT

**Ongoing Management:**
- Configuration changes are deployed via Ansible playbook
- GitHub Action validates config syntax on PR
- GitHub Action deploys config on merge to main (via Tailscale)

**Workflow Files:**
- `.github/workflows/vyos-validate.yml` - Validates VyOS config on PR
- `.github/workflows/vyos-deploy.yml` - Deploys VyOS config on merge

**VLANs Managed:**

| VLAN | Purpose | Subnet | Services |
|:-----|:--------|:-------|:---------|
| 10 | Management | 10.10.10.0/24 | SSH, IPMI, iDRAC |
| 20 | Services | 10.10.20.0/24 | NAS, DNS, DHCP |
| 30 | Platform | 10.10.30.0/24 | Platform cluster nodes |
| 40 | Cluster | 10.10.40.0/24 | Tenant cluster nodes |
| 60 | Storage | 10.10.60.0/24 | Ceph replication |

### Compute (`infrastructure/compute/talos/`)

Talos Linux configuration for the platform cluster, managed by talhelper.

**talhelper Workflow:**

1. Edit `talconfig.yaml` to define cluster nodes
2. Run `talhelper genconfig` to generate machine configs
3. Configs are encrypted with SOPS and served via NGINX during PXE boot
4. Nodes fetch their configs and join the cluster

**Files:**

- `talconfig.yaml` - Declarative cluster definition (nodes, endpoints, patches)
- `talsecret.sops.yaml` - SOPS-encrypted secrets (CA certs, tokens, etc.)
- `clusterconfig/` - Generated machine configs (gitignored, regenerated each bootstrap)
- `patches/` - Reusable machine config patches

**Why Not in clusters/platform/?**

Platform cluster configuration must exist BEFORE Kubernetes is running. It cannot be managed by Argo CD because Argo CD doesn't exist yet during bootstrap.

**Tenant Cluster Talos Configs:**

Tenant clusters use CAPI with the Talos provider, which generates machine configs automatically. Patches for tenant clusters are embedded in the TenantCluster XRD composition in `packages/infrastructure/`.

### Provisioning (`infrastructure/provisioning/tinkerbell/`)

Tinkerbell workflow templates for bare-metal OS installation.

**Templates:**

- `harvester-install.yaml` - Installs Harvester OS on MS-02 nodes
- `talos-install.yaml` - Installs Talos Linux on bare-metal nodes

**Template vs Instance:**

- **Templates** live in `infrastructure/provisioning/tinkerbell/templates/` (not cluster-specific)
- **Instances** are created via Workflow XR Claims in `clusters/platform/apps/tinkerbell/workflows/`

---

## bootstrap/ - Bootstrap and Recovery

The `bootstrap/` directory contains one-time bootstrap procedures and disaster recovery runbooks.

### Structure

```
bootstrap/
├── seed/                               # Seed phase bootstrap manifests
│   ├── tinkerbell/                     # Raw Tinkerbell deployment
│   ├── hardware/                       # UM760 hardware definition
│   ├── workflows/                      # UM760 provisioning workflow
│   └── config-server/                  # NGINX for Talos configs
│
├── genesis/                            # Bootstrap runbooks and scripts
│   ├── README.md                       # Overview and prerequisites
│   ├── 01-seed-cluster.md
│   ├── 02-deploy-argocd.md
│   ├── 03-apply-bootstrap.md
│   ├── 04-um760-provisioning.md
│   ├── 05-migrate-to-um760.md
│   ├── 06-deploy-platform.md
│   ├── 07-provision-harvester.md
│   ├── 08-expand-platform.md
│   └── scripts/
│       ├── generate-talos-config.sh
│       ├── create-seed-vm.sh
│       └── install-argocd.sh
│
└── recovery/
    ├── etcd-restore.md
    ├── platform-rebuild.md
    └── scripts/
        └── restore-etcd.sh
```

### Seed (`bootstrap/seed/`)

Raw Kubernetes manifests for the minimal seed cluster that runs on the NAS.

**Purpose:**
- Deploy Tinkerbell on resource-constrained NAS (32GB RAM)
- Provision VyOS router via PXE (establishes lab networking)
- Provision UM760 via PXE (first platform cluster node)
- Migrate to UM760 and delete seed manifests
- Replace with full platform deployment (XR-based)

**Bootstrap Targets:**

| Hardware | Workflow | Result |
|:---------|:---------|:-------|
| VP6630 | `vp6630-vyos.yaml` | VyOS router with lab networking config |
| UM760 | `um760-talos.yaml` | First platform cluster control plane node |

**Why Raw Manifests?**

The seed cluster faces a chicken-and-egg problem: Crossplane is needed to process XRs, but Crossplane is deployed via XRs. Solution: bootstrap with raw manifests, then delete and redeploy via XRs.

See [Appendix B: Bootstrap Procedure](B_bootstrap_procedure.md) for details.

### Genesis (`bootstrap/genesis/`)

Scripts for bootstrapping the lab from scratch. The bootstrap procedure uses an embedded ISO approach - no seed cluster or PXE is required for the initial VyOS and UM760 nodes.

**Bootstrap Flow:**
1. Install VyOS manually from Stream ISO, load `gateway.conf` from USB
2. Boot UM760 from embedded Talos ISO (config baked in via `labctl images sync`)
3. Deploy Argo CD via `install-argocd.sh`
4. GitOps takes over from there

See [Appendix B: Bootstrap Procedure](B_bootstrap_procedure.md) for complete step-by-step instructions.

**Scripts:**
- `install-argocd.sh` - Installs Argo CD via Helm on the single-node platform cluster

### Recovery (`bootstrap/recovery/`)

Disaster recovery procedures for catastrophic failures.

**Scenarios:**

- `etcd-restore.md` - Restore platform cluster from etcd backup
- `platform-rebuild.md` - Rebuild platform cluster from scratch

---

## Management Summary

### What is Argo CD Managed?

| Directory | Argo CD Managed | Mechanism |
|:----------|:---------------:|:----------|
| `clusters/platform/` | Yes | ApplicationSet `cluster-definitions` + `cluster-apps` |
| `clusters/harvester/` | Yes | ApplicationSet `cluster-apps` (after manual registration) |
| `clusters/media/` | Yes | ApplicationSet `cluster-definitions` + `cluster-apps` |
| `clusters/dev/` | Yes | ApplicationSet `cluster-definitions` + `cluster-apps` |
| `clusters/prod/` | Yes | ApplicationSet `cluster-definitions` + `cluster-apps` |
| `bootstrap/seed/` | One-time | Manual Application (deleted after migration) |
| `infrastructure/` | No | Manual, Ansible, talhelper |
| `packages/` | No | Built by CI/CD, consumed by Crossplane |

### What is NOT Argo CD Managed?

| Directory | Management Method | Reason |
|:----------|:------------------|:-------|
| `infrastructure/network/vyos/` | Ansible (GitHub Actions) | Pre-Kubernetes networking |
| `infrastructure/compute/talos/` | talhelper (manual) | Platform cluster bootstrap config |
| `infrastructure/provisioning/tinkerbell/` | Reference templates | Not cluster-specific |
| `packages/` | GitHub Actions (OCI build) | Source code for XRDs |
| `bootstrap/genesis/` | Manual execution | One-time bootstrap runbooks |
| `bootstrap/recovery/` | Manual execution | Disaster recovery procedures |

---

## Naming Conventions

### Directories

- **Lowercase with hyphens:** `tenant-cluster`, `core-services`, `platform-services`
- **Plural for collections:** `clusters/`, `packages/`, `workflows/`, `networks/`
- **Singular for instances:** `infrastructure/`, `bootstrap/`

### Files

- **Lowercase with hyphens:** `cluster.yaml`, `core-services.yaml`, `talos-install.yaml`
- **Descriptive names:** `ms02-node1.yaml`, `harvester.yaml`, `prometheus.yaml`
- **XR Claims:** Named after the resource: `plex.yaml`, `jellyfin.yaml`, `media.yaml`

### Git Tags

- **Crossplane packages:** `<package-name>/vX.Y.Z`
  - Example: `infrastructure/v1.2.3`, `platform/v2.0.1`
- **Semantic versioning:** MAJOR.MINOR.PATCH
  - MAJOR: Breaking changes to XRD API
  - MINOR: New features, backwards compatible
  - PATCH: Bug fixes

### Cluster Names

- **Platform cluster:** `platform`
- **Harvester cluster:** `harvester`
- **Tenant clusters:** Descriptive names based on purpose
  - Examples: `media`, `dev`, `prod`, `staging`

### XRD API Groups

- **Infrastructure:** `infrastructure.lab.gilman.io`
- **Platform:** `platform.lab.gilman.io`

### OCI Image Names

- **Pattern:** `ghcr.io/gilmanlab/xrp-<package-name>:<version>`
- **Examples:**
  - `ghcr.io/gilmanlab/xrp-infrastructure:v1.2.3`
  - `ghcr.io/gilmanlab/xrp-platform:v2.0.1`

---

## Cross-References

- [Appendix B: Bootstrap Procedure](B_bootstrap_procedure.md) - Detailed bootstrap process
- [ADR-003: GitOps Structure](../09_design_decisions/ADR-003-gitops-structure.md) - Argo CD ApplicationSet patterns
- [Concept: Crossplane Abstractions](../08_concepts/crossplane-abstractions.md) - XRD design philosophy
- [Deployment View](../07_deployment_view.md) - Physical and logical deployment architecture
