# Appendix B: Bootstrap Procedure Reference

> **Status**: Authoritative Reference
> **Date**: 2025-12-28

This document defines the architectural design of the lab bootstrap process. It describes WHAT happens at each phase and WHY, not HOW to execute commands (see `bootstrap/genesis/` runbooks for step-by-step instructions).

---

## Table of Contents

1. [Overview](#overview)
2. [Design Principles](#design-principles)
3. [Bootstrap Phases](#bootstrap-phases)
4. [Phase 1: Direct Boot (UM760)](#phase-1-direct-boot-um760)
5. [Phase 2: Single-Node Platform (UM760)](#phase-2-single-node-platform-um760)
6. [Phase 3: Harvester Online](#phase-3-harvester-online)
7. [Phase 4: Full Platform (3-Node HA)](#phase-4-full-platform-3-node-ha)
8. [Bootstrap Progression Diagram](#bootstrap-progression-diagram)
9. [Complete Step Reference](#complete-step-reference)
10. [Prerequisites](#prerequisites)
11. [Post-Bootstrap State](#post-bootstrap-state)

---

## Overview

The lab infrastructure bootstrap is a carefully orchestrated 4-phase process that progressively builds the platform from a single embedded-config ISO boot to a fully operational, highly available control plane.

### The Chicken-and-Egg Problem

The platform cluster hosts the very tools needed to provision infrastructure:
- **Crossplane** processes XR Claims to provision resources
- **CAPI** provisions tenant Kubernetes clusters
- **Tinkerbell** provisions bare-metal hardware via PXE

This creates a dependency loop: we cannot use these tools to bootstrap the platform that hosts them.

### The Solution: Embedded Configuration ISO

Rather than requiring a temporary seed cluster to serve machine configurations, we embed the UM760's Talos configuration directly into an ISO image:

1. **Direct boot** - UM760 boots from ISO with embedded machine config (no seed cluster needed)
2. **Deploy platform** - Install full platform with Crossplane/CAPI/Tinkerbell on UM760
3. **Provision Harvester** - Use Tinkerbell to PXE boot Harvester nodes
4. **Scale out** - Expand platform to 3 nodes with VMs on Harvester

By the end, the platform is self-managing via Crossplane XRs.

### How Embedded ISO Works

The `labctl images sync` pipeline uses a transform hook to create the embedded ISO:

1. Downloads base Talos ISO from Image Factory
2. Runs `talos-embed-config.sh` hook which:
   - Generates machine config using `talhelper genconfig`
   - Uses `ghcr.io/siderolabs/imager` to create new ISO with embedded config
3. Uploads the embedded ISO to S3 storage

When the UM760 boots from this ISO:
- Talos reads the embedded configuration immediately
- No network-based config fetch required
- Node bootstraps as single-node cluster automatically

---

## Design Principles

| Principle | Rationale |
|:----------|:----------|
| **Minimal dependencies** | Embedded ISO eliminates need for seed cluster or config server |
| **Progressive complexity** | Each phase adds capability only when needed |
| **GitOps from the start** | Platform cluster uses Argo CD from first boot |
| **Reproducibility** | Entire process documented and scriptable via labctl |
| **Self-healing target** | Final state is fully declarative and self-managing |

---

## Bootstrap Phases

The bootstrap is divided into 4 phases, spanning 16 discrete steps.

```
Phase 1: Direct Boot (UM760)
  Steps 1-4: Build images, boot UM760 from embedded ISO, deploy Argo CD
  Duration: ~30 minutes
  Result: Single-node platform cluster running on UM760

Phase 2: Single-Node Platform (UM760)
  Steps 5-9: Deploy full platform with Crossplane/CAPI/Tinkerbell, provision Harvester
  Duration: ~2 hours
  Result: Platform cluster with Crossplane, CAPI, Harvester provisioned

Phase 3: Harvester Online
  Steps 10-13: Register Harvester, create platform VMs
  Duration: ~30 minutes
  Result: CP-2 and CP-3 join platform cluster

Phase 4: Full Platform (3-Node HA)
  Steps 14-16: Deploy remaining services, steady state
  Duration: ~30 minutes
  Result: Full platform operational, ready for tenant clusters
```

**Total Duration:** ~3.5 hours (mostly waiting for Harvester installation)

---

## Phase 1: Direct Boot (UM760)

**Goal:** Bootstrap the UM760 as the first platform node using an ISO with embedded machine configuration, eliminating the need for a temporary seed cluster.

**Key Innovation:**
The embedded ISO approach removes the complexity of the previous seed cluster method:
- No NAS VM required for initial bootstrap
- No Tinkerbell needed for UM760 provisioning
- Configuration is baked into the ISO, not fetched over network
- Single boot operation creates a functional single-node cluster

**What Runs After Boot:**
- Single-node Talos Kubernetes cluster
- Ready for Argo CD deployment
- Full 64GB RAM available for platform services

### Step 1: Sync Images with labctl

**Purpose:** Build all required boot images including the embedded Talos ISO.

**Mechanism:**
- Run `labctl images sync` to process `images/images.yaml`
- For `talos-um760` image:
  1. Downloads base Talos ISO from Image Factory
  2. Runs `talos-embed-config.sh` transform hook
  3. Hook generates machine config via `talhelper genconfig`
  4. Hook creates new ISO with embedded config using Talos imager
  5. Uploads embedded ISO to S3 storage
- For `vyos-stream` image:
  1. Downloads VyOS ISO
  2. Runs integration tests
  3. Uploads to S3 storage

**Transform Hook Details:**
```bash
# The hook is invoked as:
./images/hooks/talos-embed-config.sh <downloaded-iso-path> cp-1

# It performs:
# 1. talhelper genconfig --out-dir /tmp/clusterconfig
# 2. docker run ghcr.io/siderolabs/imager:v1.9.1 iso \
#      --embedded-config-path /tmp/clusterconfig/platform-cp-1.yaml
# 3. Replaces downloaded ISO with embedded version
```

**Result:**
- `talos/talos-1.9.1-um760.iso` uploaded to S3 (with embedded CP-1 config)
- `talos/talos-1.9.1-metal-amd64.iso` uploaded to S3 (vanilla, for other uses)
- `vyos/vyos-2025.11-generic-amd64.iso` uploaded to S3

### Step 2: Prepare VyOS Router

**Purpose:** Establish lab networking before UM760 bootstrap.

**Mechanism:**
- VyOS is pre-configured (either from previous install or manual setup)
- Required VLANs: 10 (mgmt), 20 (services), 30 (platform), 40 (cluster), 60 (storage)
- UM760 must have network connectivity on VLAN 30

**Note:** VyOS provisioning via Tinkerbell happens in Phase 2 after the platform cluster is running. For initial bootstrap, VyOS must already be configured.

**Result:**
- Lab networking operational
- UM760 can reach the network

### Step 3: Boot UM760 from Embedded ISO

**Purpose:** Bootstrap the UM760 as a single-node platform cluster.

**Mechanism:**
1. Download embedded ISO from S3 storage
2. Write ISO to USB drive or mount via IPMI virtual media
3. Boot UM760 from ISO
4. Talos reads embedded configuration automatically:
   - Hostname: `cp-1`
   - IP: `10.10.30.10` (VLAN 30)
   - Role: Control plane + worker (single-node mode)
   - Cluster name: `platform`
5. Talos installs to disk and reboots
6. Single-node cluster bootstraps automatically

**What the Embedded Config Contains:**
```yaml
# From infrastructure/compute/talos/talconfig.yaml
clusterName: platform
endpoint: https://10.10.30.10:6443
nodes:
  - hostname: cp-1
    ipAddress: 10.10.30.10
    controlPlane: true
    networkInterfaces:
      - vlans:
          - vlanId: 30
            addresses: [10.10.30.10/24]
```

**Timeline:**
- ISO boot: ~2 minutes
- Talos install to disk: ~5 minutes
- Cluster bootstrap: ~3 minutes
- **Total: ~10 minutes**

**Result:**
- Single-node Talos cluster running on UM760
- Kubernetes API available at `https://10.10.30.10:6443`
- Ready for Argo CD deployment

### Step 4: Deploy Argo CD

**Purpose:** Establish GitOps controller to manage all subsequent deployments.

**Mechanism:**
- Extract kubeconfig using `talosctl kubeconfig`
- Install Argo CD via Helm CLI
- Use `bootstrap/genesis/scripts/install-argocd.sh`
- Register platform cluster with itself (in-cluster registration)

**Why Argo CD First:**
- All subsequent components deployed via GitOps
- Enables declarative, version-controlled infrastructure
- Argo CD is lightweight and works on single-node cluster

**Configuration:**
```yaml
# Created by install-argocd.sh
apiVersion: v1
kind: Secret
metadata:
  name: platform
  namespace: argocd
  labels:
    argocd.argoproj.io/secret-type: cluster
    lab.gilman.io/cluster-name: platform
stringData:
  name: platform
  server: https://kubernetes.default.svc  # In-cluster
```

**Result:**
- Argo CD running on platform cluster
- Ready to sync GitOps applications

---

## Phase 2: Single-Node Platform (UM760)

**Goal:** Deploy full platform stack on the single-node cluster and provision Harvester.

**Starting State:**
- Single-node Talos cluster running on UM760
- Argo CD installed and operational
- No Crossplane, CAPI, or Tinkerbell yet

### Step 5: Apply Platform Configuration

**Purpose:** Deploy full platform cluster configuration via Crossplane XRs.

**Mechanism:**
- Create ApplicationSet: `kubectl apply -f argocd/applicationsets/cluster-definitions.yaml`
- Argo CD discovers `clusters/platform/` directory
- Syncs `core.yaml` and `platform.yaml` to platform cluster

**What Gets Deployed:**

| File | XR Type | What It Provisions |
|:-----|:--------|:-------------------|
| `clusters/platform/core.yaml` | CoreServices | Crossplane, CAPI, cert-manager, external-dns, Istio |
| `clusters/platform/platform.yaml` | PlatformServices | Zitadel, OpenBAO, Vault Secrets Operator |

**Sync Waves:**
- Wave 0: `core.yaml` (Crossplane must be running first)
- Wave 1: `platform.yaml` (depends on Crossplane)
- Wave 2: `apps/*` (depends on CoreServices)

**Result:**
- Crossplane is running
- CAPI is installed (but no clusters yet)
- cert-manager, external-dns, Istio deployed
- Zitadel and OpenBAO deployed

### Step 6: Crossplane + Tinkerbell (XRD)

**Purpose:** Redeploy Tinkerbell via proper Crossplane abstraction.

**Mechanism:**
- ApplicationSet discovers `clusters/platform/apps/tinkerbell/`
- Argo CD syncs Hardware XRs and Workflow XRs
- Crossplane processes XRs and creates Tinkerbell Hardware/Workflow CRDs

**Files Synced:**
```
clusters/platform/apps/tinkerbell/
├── hardware/
│   ├── ms02-node1.yaml         # Hardware XR for Harvester node 1
│   ├── ms02-node2.yaml         # Hardware XR for Harvester node 2
│   ├── ms02-node3.yaml         # Hardware XR for Harvester node 3
│   └── um760.yaml              # Hardware XR for UM760 (already provisioned)
└── workflows/
    ├── harvester.yaml          # Workflow XR for Harvester installation
    └── talos.yaml              # Workflow XR for Talos installation
```

**Result:**
- Tinkerbell redeployed via XRs (now permanent)
- Hardware definitions registered for MS-02 nodes (Harvester cluster)
- Workflows ready to provision Harvester

### Step 7: Provision VyOS via Tinkerbell

**Purpose:** Provision the VyOS router using Tinkerbell now that the platform is running.

**Mechanism:**
1. Power on VP6630 with PXE boot enabled
2. Tinkerbell detects hardware (MAC addresses match Hardware XRs)
3. Executes VyOS installation workflow:
   - Downloads VyOS image
   - Writes VyOS to disk
   - Applies VyOS config
   - Reboots into VyOS
4. VyOS boots with pre-configured lab networking

**Note:** If VyOS was already manually configured in Phase 1, this step can be skipped or used to reprovision with the official workflow.

**Result:**
- VyOS router provisioned via GitOps
- Lab networking fully managed

### Step 8: Provision Harvester

**Purpose:** Install Harvester OS on MS-02 nodes to create HCI cluster.

**Mechanism:**
1. Power on MS-02 nodes with PXE boot enabled
2. Tinkerbell detects hardware (MAC addresses match Hardware XRs)
3. Executes Harvester installation workflow:
   - Downloads Harvester ISO
   - Writes Harvester to disk
   - Applies Harvester config (cluster token, network config)
   - Reboots into Harvester
4. Harvester cluster self-forms (3-node)

**Harvester Installation:**
- **Duration:** ~1.5 hours (Harvester install is slow)
- **Result:** 3-node Harvester cluster running on MS-02 hardware
- **Services:** Longhorn (storage), Harvester VMs, Harvester networking

**Harvester Cluster Details:**

| Node | Hardware | Role | IP (VLAN 10 - Mgmt) |
|:-----|:---------|:-----|:--------------------|
| ms02-node1 | MS-02 | Control Plane + Compute | 10.10.10.11 |
| ms02-node2 | MS-02 | Control Plane + Compute | 10.10.10.12 |
| ms02-node3 | MS-02 | Control Plane + Compute | 10.10.10.13 |

**Result:**
- Harvester cluster operational
- Ready to host VMs
- Platform cluster still single-node (UM760)

---

## Phase 3: Harvester Online

**Goal:** Register Harvester with Argo CD, deploy VM configurations, and create CP-2/CP-3 VMs to expand platform cluster.

**Why This Phase:**
- Platform cluster is still single-node (no HA)
- Need VMs on Harvester to add CP-2 and CP-3
- Harvester must be registered with Argo CD before it can be managed

### Step 9: Register Harvester with Argo CD

**Purpose:** Allow Argo CD to deploy resources to Harvester cluster.

**Mechanism:**
- Extract Harvester kubeconfig (from Harvester UI or API)
- Create Argo CD cluster Secret with Harvester endpoint and credentials
- Label Secret with `lab.gilman.io/cluster-name: harvester`

**Cluster Secret:**
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: harvester
  namespace: argocd
  labels:
    argocd.argoproj.io/secret-type: cluster
    lab.gilman.io/cluster-name: harvester
type: Opaque
stringData:
  name: harvester
  server: https://10.10.10.11:6443  # Harvester API endpoint
  config: |
    {
      "tlsClientConfig": {
        "caData": "...",
        "certData": "...",
        "keyData": "..."
      }
    }
```

**Result:**
- Harvester appears in Argo CD cluster list
- ApplicationSet can now route apps to Harvester

### Step 10: Argo CD Syncs `clusters/harvester/`

**Purpose:** Deploy Harvester network configuration, VM images, and VM definitions.

**Mechanism:**
- ApplicationSet matrix generator discovers Harvester cluster + `clusters/harvester/` directory
- Argo CD syncs to Harvester cluster (not platform)
- Deploys raw Harvester CRDs (ClusterNetwork, VlanConfig, VirtualMachineImage, VirtualMachine)

**What Gets Deployed:**

**Networks (clusters/harvester/config/networks/):**

| File | VLAN | Purpose | Subnet |
|:-----|:----:|:--------|:-------|
| `mgmt.yaml` | 10 | Management network | 10.10.10.0/24 |
| `platform.yaml` | 30 | Platform cluster network | 10.10.30.0/24 |
| `cluster.yaml` | 40 | Tenant cluster network | 10.10.40.0/24 |
| `storage.yaml` | 60 | Ceph replication network | 10.10.60.0/24 |

**Images (clusters/harvester/config/images/):**

| File | Image | Purpose |
|:-----|:------|:--------|
| `talos-1.9.yaml` | Talos 1.9 VM image | OS for platform cluster VMs |

**VMs (clusters/harvester/vms/platform/):**

| File | VM Name | vCPU | RAM | Disk | Network | MAC Address |
|:-----|:--------|:-----|:----|:-----|:--------|:------------|
| `cp-2.yaml` | platform-cp-2 | 8 | 16GB | 100GB | VLAN 30 | 52:54:00:xx:xx:02 |
| `cp-3.yaml` | platform-cp-3 | 8 | 16GB | 100GB | VLAN 30 | 52:54:00:xx:xx:03 |

**Result:**
- Harvester networks configured
- Talos VM image available
- CP-2 and CP-3 VMs created (powered off initially)

### Step 11: CP-2, CP-3 VMs Created

**Purpose:** Create VirtualMachine resources on Harvester for platform cluster nodes.

**Mechanism:**
- Harvester processes VirtualMachine CRDs
- Allocates resources from Longhorn storage
- Attaches to VLAN 30 (platform network)
- VMs created in powered-off state

**VM Specifications:**

| Parameter | CP-2 | CP-3 |
|:----------|:-----|:-----|
| CPU | 8 vCPU | 8 vCPU |
| RAM | 16GB | 16GB |
| Disk | 100GB (Longhorn) | 100GB (Longhorn) |
| Network | VLAN 30 | VLAN 30 |
| MAC | 52:54:00:xx:xx:02 | 52:54:00:xx:xx:03 |
| Boot Order | Network (PXE) → Disk | Network (PXE) → Disk |

**Result:**
- VMs exist but not yet running
- Ready for PXE boot

### Step 12: CP-2, CP-3 PXE Boot

**Purpose:** Provision CP-2 and CP-3 with Talos OS and join platform cluster.

**Mechanism:**
1. Power on CP-2 and CP-3 VMs
2. VMs PXE boot (first boot device is network)
3. Tinkerbell detects VMs (MAC addresses match Hardware XRs)
4. Executes Talos installation workflow:
   - Downloads Talos image
   - Fetches machine config from NGINX (cp-2.yaml, cp-3.yaml)
   - Writes Talos to disk
   - Reboots into Talos
5. Talos bootstraps and joins platform cluster as CP-2, CP-3

**Timeline:**
- PXE boot (per VM): ~2 minutes
- Talos install (per VM): ~5 minutes
- Cluster join (per VM): ~2 minutes
- **Total: ~20 minutes** (VMs can boot in parallel)

**Result:**
- Platform cluster now has 3 control plane nodes: CP-1 (UM760), CP-2 (VM), CP-3 (VM)
- High availability achieved (etcd quorum: 2/3)

---

## Phase 4: Full Platform (3-Node HA)

**Goal:** Deploy remaining platform services and reach steady state.

**Why This Phase:**
- Platform cluster is now HA (3 control plane nodes)
- Safe to deploy production workloads
- Infrastructure complete, ready for tenant clusters

### Step 13: Deploy Remaining Platform Services

**Purpose:** Activate all platform capabilities (observability, policy, etc.).

**Mechanism:**
- ApplicationSet discovers `clusters/platform/apps/*`
- Argo CD syncs Application XRs to platform cluster
- Crossplane processes XRs and deploys Helm releases

**Services Deployed:**

**Observability (clusters/platform/apps/observability/):**

| File | Application | Purpose |
|:-----|:------------|:--------|
| `prometheus.yaml` | Prometheus | Metrics collection and alerting |
| `grafana.yaml` | Grafana | Metrics visualization |
| `loki.yaml` | Loki | Log aggregation |

**CAPI (clusters/platform/apps/capi/):**

| File | Purpose |
|:-----|:--------|
| `providers.yaml` | Install CAPI providers (Harvester, Talos) |
| `harvester-config.yaml` | Configure Harvester provider with credentials |

**Result:**
- Full observability stack operational
- CAPI ready to provision tenant clusters
- Platform services fully deployed

### Step 14: Steady State

**Purpose:** Validate that all platform components are healthy and operational.

**Validation Checklist:**

| Component | Check | Expected Result |
|:----------|:------|:----------------|
| Platform cluster | `kubectl get nodes` | 3 nodes (CP-1, CP-2, CP-3) all Ready |
| Harvester cluster | `kubectl get nodes --kubeconfig harvester` | 3 nodes (ms02-1, ms02-2, ms02-3) all Ready |
| Crossplane | `kubectl get xrd` | All XRDs Established |
| CAPI | `kubectl get providers -A` | Harvester and Talos providers Installed |
| Argo CD | UI / `argocd app list` | All apps Healthy, Synced |
| Tinkerbell | `kubectl get hardware -n tinkerbell` | All hardware Ready |
| Istio | `kubectl get pods -n istio-system` | All pods Running |
| Zitadel | `curl https://auth.lab.local` | 200 OK |
| OpenBAO | `curl https://vault.lab.local` | 200 OK |

**Result:**
- Platform cluster is fully operational
- All services healthy
- Ready for tenant clusters

### Step 15: Tenant Clusters

**Purpose:** Begin provisioning application workload clusters.

**Mechanism:**
1. Create TenantCluster XR in `clusters/media/cluster.yaml`
2. Commit and push to Git
3. Argo CD syncs XR to platform cluster
4. Crossplane processes TenantCluster XR:
   - Creates CAPI Cluster resource
   - CAPI Harvester provider creates VMs on Harvester
   - CAPI Talos provider generates machine configs
   - VMs PXE boot, install Talos, join cluster
   - CAPI generates kubeconfig
   - Crossplane creates Argo CD cluster Secret
5. Argo CD discovers new cluster
6. ApplicationSet syncs `clusters/media/apps/*` to media cluster

**Example TenantCluster XR:**
```yaml
apiVersion: infrastructure.lab.gilman.io/v1alpha1
kind: TenantCluster
metadata:
  name: media
  namespace: default
spec:
  controlPlane:
    count: 3
    cpu: 4
    memory: 8Gi
    disk: 100Gi
  workers:
    count: 3
    cpu: 8
    memory: 32Gi
    disk: 500Gi
  network:
    vlan: 40
    subnet: 10.10.40.0/24
```

**Timeline:**
- VM creation: ~5 minutes
- Talos install: ~10 minutes
- Cluster ready: ~15 minutes
- **Total: ~30 minutes per cluster**

**Result:**
- Tenant cluster operational
- Automatically registered with Argo CD
- Applications deployed via GitOps

---

## Bootstrap Progression Diagram

```
┌─────────────────────────────────────────────────────────────────────────┐
│                    PHASE 1: DIRECT BOOT (UM760)                         │
│                                                                         │
│  Step 1: Sync Images with labctl                                       │
│           → Downloads base Talos ISO                                    │
│           → Runs transform hook to embed machine config                 │
│           → Uploads embedded ISO to S3                                  │
│           ↓                                                             │
│  Step 2: Prepare VyOS Router (manual or pre-existing)                  │
│           ↓                                                             │
│  Step 3: Boot UM760 from Embedded ISO                                  │
│           → Talos reads embedded config                                 │
│           → Installs to disk, bootstraps cluster                        │
│           ↓                                                             │
│  Step 4: Deploy Argo CD (Helm CLI)                                     │
│                                                                         │
│  Result: Single-node platform cluster on UM760                         │
└─────────────────────────────────────────────────────────────────────────┘
                               ↓
┌─────────────────────────────────────────────────────────────────────────┐
│                  PHASE 2: SINGLE-NODE PLATFORM (UM760)                  │
│                                                                         │
│  Step 5: Apply Platform Configuration (clusters/platform/)             │
│           → core.yaml (CoreServices XR)                                 │
│           → platform.yaml (PlatformServices XR)                         │
│           ↓                                                             │
│  Step 6: Crossplane + Tinkerbell (XRD-based)                           │
│           ↓                                                             │
│  Step 7: Provision VyOS via Tinkerbell (optional)                      │
│           ↓                                                             │
│  Step 8: Provision Harvester (Tinkerbell PXE boots MS-02 nodes)        │
│           → 3-node Harvester cluster online (~1.5 hours)                │
│                                                                         │
│  Result: Platform cluster (1 node), Harvester cluster (3 nodes)        │
└─────────────────────────────────────────────────────────────────────────┘
                               ↓
┌─────────────────────────────────────────────────────────────────────────┐
│                      PHASE 3: HARVESTER ONLINE                          │
│                                                                         │
│  Step 9: Register Harvester with Argo CD (cluster Secret)              │
│           ↓                                                             │
│  Step 10: Argo CD Syncs clusters/harvester/                            │
│           → Networks (VLANs 10, 30, 40, 60)                             │
│           → Images (Talos 1.9)                                          │
│           → VMs (CP-2, CP-3)                                            │
│           ↓                                                             │
│  Step 11: CP-2, CP-3 VMs Created (powered off)                         │
│           ↓                                                             │
│  Step 12: CP-2, CP-3 PXE Boot → Talos installed → Join cluster         │
│                                                                         │
│  Result: Platform cluster (3 nodes HA), Harvester cluster (3 nodes)    │
└─────────────────────────────────────────────────────────────────────────┘
                               ↓
┌─────────────────────────────────────────────────────────────────────────┐
│                  PHASE 4: FULL PLATFORM (3-NODE HA)                     │
│                                                                         │
│  Step 13: Deploy Remaining Platform Services                           │
│           → Observability (Prometheus, Grafana, Loki)                   │
│           → CAPI providers (Harvester, Talos)                           │
│           ↓                                                             │
│  Step 14: Steady State - Validate all components healthy               │
│           ↓                                                             │
│  Step 15: Tenant Clusters - Provision via TenantCluster XR             │
│           → media cluster (3 CP + 3 workers)                            │
│           → dev cluster (1 CP + 2 workers)                              │
│           → prod cluster (3 CP + 5 workers)                             │
│                                                                         │
│  Result: Full lab operational, ready for application workloads         │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## Complete Step Reference

| Phase | Step | Name | Duration | Purpose |
|:------|:-----|:-----|:---------|:--------|
| 1 | 1 | Sync Images with labctl | 15 min | Build embedded ISO via transform hook |
| 1 | 2 | Prepare VyOS Router | 5 min | Ensure lab networking is ready |
| 1 | 3 | Boot UM760 from Embedded ISO | 10 min | Bootstrap single-node platform cluster |
| 1 | 4 | Deploy Argo CD | 5 min | Install GitOps controller |
| 2 | 5 | Apply Platform Configuration | 10 min | Deploy CoreServices and PlatformServices XRs |
| 2 | 6 | Crossplane + Tinkerbell (XRD) | 10 min | Deploy Tinkerbell via XRs |
| 2 | 7 | Provision VyOS via Tinkerbell | 6 min | (Optional) Reprovision VyOS via GitOps |
| 2 | 8 | Provision Harvester | 90 min | PXE boot MS-02 nodes with Harvester OS |
| 3 | 9 | Register Harvester with Argo CD | 5 min | Create cluster Secret for Harvester |
| 3 | 10 | Argo CD Syncs clusters/harvester/ | 5 min | Deploy networks, images, VM definitions |
| 3 | 11 | CP-2, CP-3 VMs Created | 5 min | Harvester creates VM resources |
| 3 | 12 | CP-2, CP-3 PXE Boot | 20 min | Provision VMs with Talos, join platform cluster |
| 4 | 13 | Deploy Remaining Platform Services | 15 min | Observability, CAPI providers |
| 4 | 14 | Steady State | 10 min | Validate all components healthy |
| 4 | 15 | Tenant Clusters | 30 min/cluster | Provision application workload clusters |

**Total Duration (Phases 1-3):** ~3 hours
**Phase 4:** Ongoing (as tenant clusters are added)

---

## Prerequisites

Before beginning the bootstrap, ensure the following are in place:

### Hardware

| Component | Requirement | Purpose |
|:----------|:------------|:--------|
| **VP6630** | Minisforum VP6630 (VyOS router) | Lab gateway, VLAN routing, DHCP relay |
| **UM760** | Minisforum UM760 (64GB RAM, 1TB SSD) | First platform node (bare-metal) |
| **MS-02 (×3)** | Minisforum MS-02 (64GB RAM, 1TB NVMe each) | Harvester HCI cluster |
| **Network** | Managed switch with VLAN support | Layer 2 switching, trunk ports |

### Network

| VLAN | Subnet | Purpose | DHCP |
|:-----|:-------|:--------|:-----|
| 10 | 10.10.10.0/24 | Management | Static IPs |
| 20 | 10.10.20.0/24 | Services (NAS, DNS) | Static IPs |
| 30 | 10.10.30.0/24 | Platform cluster | DHCP (Tinkerbell) |
| 40 | 10.10.40.0/24 | Tenant clusters | DHCP (Tinkerbell) |
| 60 | 10.10.60.0/24 | Storage replication | Static IPs |

**Note:** VyOS must be configured before UM760 bootstrap to provide network connectivity. It can be:
- Manually configured initially, then reprovisioned via Tinkerbell in Phase 2
- Pre-configured from a previous installation

### Software

| Tool | Version | Purpose |
|:-----|:--------|:--------|
| Docker | v24.0.0+ | Run Talos imager and vyos-build containers |
| labctl | latest | Sync images with transform hooks |
| talhelper | v3.0.0+ | Generate Talos machine configs |
| SOPS | v3.9.0+ | Encrypt Talos secrets |
| kubectl | v1.31.0+ | Kubernetes CLI |
| Helm | v3.16.0+ | Install Argo CD |
| talosctl | v1.9.0+ | Talos cluster management |

### Credentials

- **GitHub** - Personal access token with repo access (for Argo CD)
- **SOPS** - Age key for encrypting/decrypting secrets
- **S3/iDrive e2** - API credentials for image storage
- **Harvester** - Cluster token (generated during install)

### Git Repository

- Clone `https://github.com/gilmanlab/lab.git`
- Checkout branch: `main` (or feature branch for testing)
- Update `infrastructure/compute/talos/talconfig.yaml` with actual MAC addresses
- Commit and push changes

---

## Post-Bootstrap State

After completing all 15 steps, the lab infrastructure is in the following state:

### Clusters

| Cluster | Nodes | Purpose | State |
|:--------|:------|:--------|:------|
| **Platform** | 3 (CP-1, CP-2, CP-3) | Control plane, shared services | Operational |
| **Harvester** | 3 (ms02-1, ms02-2, ms02-3) | HCI, VM hosting | Operational |
| **Tenant** | 0 (ready for provisioning) | Application workloads | Ready |

### Platform Cluster Details

**Nodes:**

| Node | Type | Hardware | IP (VLAN 30) | Role |
|:-----|:-----|:---------|:-------------|:-----|
| CP-1 | Bare-metal | UM760 | 10.10.30.10 | Control Plane + Worker |
| CP-2 | VM | Harvester VM | 10.10.30.11 | Control Plane |
| CP-3 | VM | Harvester VM | 10.10.30.12 | Control Plane |

**Services Running:**

| Service | Namespace | Purpose |
|:--------|:----------|:--------|
| Argo CD | argocd | GitOps controller |
| Crossplane | crossplane-system | Infrastructure provisioning |
| CAPI | capi-system | Cluster provisioning |
| Tinkerbell | tinkerbell | PXE provisioning |
| Istio | istio-system | Service mesh |
| cert-manager | cert-manager | Certificate management |
| external-dns | external-dns | DNS automation |
| Zitadel | zitadel | Identity provider |
| OpenBAO | openbao | Secrets management |
| Vault Secrets Operator | vault-secrets-operator | Secret injection |
| Prometheus | observability | Metrics collection |
| Grafana | observability | Metrics visualization |
| Loki | observability | Log aggregation |

### Harvester Cluster Details

**Nodes:**

| Node | Hardware | IP (VLAN 10) | Storage | Role |
|:-----|:---------|:-------------|:--------|:-----|
| ms02-1 | MS-02 | 10.10.10.11 | 1TB NVMe | Control Plane + Compute |
| ms02-2 | MS-02 | 10.10.10.12 | 1TB NVMe | Control Plane + Compute |
| ms02-3 | MS-02 | 10.10.10.13 | 1TB NVMe | Control Plane + Compute |

**Storage:**
- Longhorn replicated storage (3 replicas across nodes)
- Total capacity: ~2.7TB (3×1TB - overhead)

**VMs Running:**

| VM | vCPU | RAM | Disk | Network | Purpose |
|:---|:-----|:----|:-----|:--------|:--------|
| platform-cp-2 | 8 | 16GB | 100GB | VLAN 30 | Platform control plane node 2 |
| platform-cp-3 | 8 | 16GB | 100GB | VLAN 30 | Platform control plane node 3 |

### GitOps State

**Argo CD Applications:**

| Application | Path | Destination | Status |
|:------------|:-----|:------------|:-------|
| cluster-platform | clusters/platform/ | platform | Synced, Healthy |
| platform-tinkerbell | clusters/platform/apps/tinkerbell/ | platform | Synced, Healthy |
| platform-observability | clusters/platform/apps/observability/ | platform | Synced, Healthy |
| platform-capi | clusters/platform/apps/capi/ | platform | Synced, Healthy |
| cluster-harvester | clusters/harvester/ | harvester | Synced, Healthy |

**Registered Clusters:**

| Cluster | Server | Namespace | Label |
|:--------|:-------|:----------|:------|
| platform | https://kubernetes.default.svc | argocd | lab.gilman.io/cluster-name: platform |
| harvester | https://10.10.10.11:6443 | argocd | lab.gilman.io/cluster-name: harvester |

### Crossplane State

**Installed Packages:**

| Package | Version | Purpose |
|:--------|:--------|:--------|
| xrp-infrastructure | v1.0.0 | TenantCluster, Hardware, Workflow XRDs |
| xrp-platform | v1.0.0 | CoreServices, PlatformServices, Application, Database XRDs |

**XRDs Established:**

| XRD | API Group | Kind |
|:----|:----------|:-----|
| TenantCluster | infrastructure.lab.gilman.io | TenantCluster |
| Hardware | infrastructure.lab.gilman.io | Hardware |
| Workflow | infrastructure.lab.gilman.io | Workflow |
| CoreServices | platform.lab.gilman.io | CoreServices |
| PlatformServices | platform.lab.gilman.io | PlatformServices |
| Application | platform.lab.gilman.io | Application |
| Database | platform.lab.gilman.io | Database |

### Next Steps

The platform is now ready for tenant cluster provisioning:

1. Define tenant cluster in `clusters/<name>/cluster.yaml`
2. Define core services in `clusters/<name>/core.yaml`
3. Define applications in `clusters/<name>/apps/*/`
4. Commit and push to Git
5. Argo CD automatically syncs and provisions

**Example workflow:**
```bash
# Create media cluster
mkdir -p clusters/media/apps/plex
cat > clusters/media/cluster.yaml <<EOF
apiVersion: infrastructure.lab.gilman.io/v1alpha1
kind: TenantCluster
metadata:
  name: media
spec:
  controlPlane:
    count: 3
    cpu: 4
    memory: 8Gi
  workers:
    count: 3
    cpu: 8
    memory: 32Gi
EOF

git add clusters/media/
git commit -m "Add media cluster"
git push

# Wait ~30 minutes for cluster to provision
# Argo CD automatically registers cluster and deploys apps
```

---

## Cross-References

- [Appendix A: Repository Structure](A_repository_structure.md) - Directory layout and organization
- [ADR-004: Platform Cluster Deployment](../09_design_decisions/ADR-004-platform-deployment.md) - Platform cluster architecture decisions
- [Concept: Crossplane Abstractions](../08_concepts/crossplane-abstractions.md) - XRD design philosophy
- [Deployment View](../07_deployment_view.md) - Physical and logical deployment architecture
- [Building Block: Tinkerbell](../05_building_blocks/tinkerbell.md) - PXE provisioning system
- [Building Block: Harvester](../05_building_blocks/harvester.md) - HCI platform
- [Genesis Runbooks](../../bootstrap/genesis/README.md) - Step-by-step command execution guide
