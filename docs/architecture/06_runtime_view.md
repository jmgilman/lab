# 06. Runtime View

This section describes key runtime scenarios — how the system's building blocks interact during critical operations.

---

## 1. Genesis Bootstrap

The "Genesis" sequence bootstraps the entire infrastructure from bare metal to a fully operational Platform Cluster using an embedded configuration ISO approach that eliminates the need for a temporary seed cluster.

### Prerequisites
- Physical hardware cabled and powered
- VyOS Stream ISO downloaded via `labctl images sync`
- Talos ISO with embedded machine configuration (built via `labctl images sync`)
- USB drives for VyOS and Talos installation

### Sequence

```mermaid
sequenceDiagram
    participant VyOS as VP6630 (VyOS)
    participant UM as UM760
    participant Argo as Argo CD
    participant Tink as Tinkerbell
    participant MS as MS-02 (x3)
    participant Harv as Harvester
    participant Plat as Platform Cluster

    Note over VyOS: Phase 1: Direct Boot
    VyOS->>VyOS: Boot from Stream ISO (USB)
    VyOS->>VyOS: Load gateway.conf from USB
    Note over VyOS: VLANs + NAT + DHCP relay active

    UM->>UM: Boot from embedded Talos ISO (USB)
    Note over UM: Config embedded in ISO
    UM->>UM: Install to disk, bootstrap cluster
    UM->>Argo: Deploy Argo CD (Helm)

    Note over UM: Phase 2: Single-Node Platform
    Argo->>UM: Sync clusters/platform/
    UM->>UM: Deploy Crossplane + XRDs
    UM->>Tink: Deploy Tinkerbell via XRs

    Note over MS: Phase 3: Harvester Online
    MS->>Tink: PXE boot (x3)
    Tink->>MS: Provision Harvester
    MS->>Harv: Form HA cluster
    Argo->>Harv: Register as managed cluster
    Argo->>Harv: Sync clusters/harvester/

    Note over Plat: Phase 4: Full Platform
    Harv->>Harv: Create CP-2, CP-3 VMs
    Harv-->>Tink: VMs PXE boot
    Tink->>Harv: Provision Talos on VMs
    Harv-->>UM: VMs join platform cluster
    UM->>Plat: 3-node Platform Cluster formed
    Plat->>Plat: Deploy remaining services
```

### Phase Summary

| Phase | Action | Result |
|:---|:---|:---|
| **1. Direct Boot** | Install VyOS from ISO, boot UM760 from embedded ISO, deploy Argo CD | Single-node platform with VyOS networking |
| **2. Single-Node Platform** | Deploy Crossplane + Tinkerbell via GitOps | Platform ready to provision hardware |
| **3. Harvester Online** | Provision 3x MS-02 via Tinkerbell, register with Argo CD | HCI cluster managed by Argo CD |
| **4. Full Platform** | Add 2 Harvester VMs to platform cluster | 3-node HA Platform Cluster |

---

## 2. Cluster Lifecycle

Downstream clusters are created, scaled, and destroyed declaratively via Git.

### Create Cluster

```mermaid
sequenceDiagram
    participant Dev as Developer
    participant Git as GitHub
    participant Argo as Argo CD
    participant CAPI as Cluster API
    participant Harv as Harvester
    participant Talos as Talos API

    Dev->>Git: Commit Cluster manifest
    Git->>Argo: Webhook / Poll
    Argo->>CAPI: Sync Cluster CRD
    CAPI->>Harv: Create VMs (CP + Workers)
    Harv-->>CAPI: VMs running
    CAPI->>Talos: Apply machine configs
    Talos-->>CAPI: Nodes joined
    CAPI-->>Argo: Cluster Ready
    Argo->>Argo: Sync workloads to new cluster
```

### Scale Cluster

```mermaid
sequenceDiagram
    participant Dev as Developer
    participant Git as GitHub
    participant Argo as Argo CD
    participant CAPI as Cluster API
    participant Harv as Harvester

    Dev->>Git: Update MachineDeployment replicas
    Git->>Argo: Sync
    Argo->>CAPI: Update MachineDeployment
    alt Scale Up
        CAPI->>Harv: Create new VM(s)
        Harv-->>CAPI: VMs joined cluster
    else Scale Down
        CAPI->>CAPI: Cordon & drain node
        CAPI->>Harv: Delete VM
    end
```

### Delete Cluster

```mermaid
sequenceDiagram
    participant Dev as Developer
    participant Git as GitHub
    participant Argo as Argo CD
    participant CAPI as Cluster API
    participant Harv as Harvester

    Dev->>Git: Remove Cluster manifest
    Git->>Argo: Sync (prune enabled)
    Argo->>CAPI: Delete Cluster CRD
    CAPI->>CAPI: Delete Machines
    CAPI->>Harv: Delete VMs
    Harv-->>CAPI: Resources cleaned up
```

---

## 3. GitOps Sync Flow

All configuration changes flow through Git. This is the standard path for deploying or updating workloads.

### Application Deployment

```mermaid
sequenceDiagram
    participant Dev as Developer
    participant Git as GitHub
    participant Argo as Argo CD (Platform)
    participant K8s as Downstream Cluster

    Dev->>Git: Commit application manifest
    Git->>Argo: Webhook notification
    Argo->>Argo: ApplicationSet detects new app
    Argo->>K8s: Apply manifests (via kubeconfig)
    K8s-->>Argo: Resources created
    Argo->>Argo: Report sync status: Healthy
```

### Sync Modes

| Mode | Description |
|:---|:---|
| **Auto-Sync** | Argo automatically applies changes on Git commit |
| **Manual Sync** | Operator triggers sync via UI/CLI (for sensitive changes) |
| **Prune** | Argo deletes resources removed from Git |
| **Self-Heal** | Argo reverts manual cluster changes to match Git |

### Drift Detection

```
┌─────────────────┐       ┌─────────────────┐
│     GitHub      │       │    Argo CD      │
│  (Desired State)│◀─────▶│ (Reconciler)    │
└─────────────────┘       └────────┬────────┘
                                   │ Compare
                                   ▼
                          ┌─────────────────┐
                          │   Cluster       │
                          │ (Actual State)  │
                          └─────────────────┘
                                   │
                          Drift? ──┼── Yes → Auto-correct
                                   └── No  → Healthy
```

If drift is detected (manual `kubectl` changes), Argo CD can:
- **Alert**: Notify operator of out-of-sync state
- **Self-Heal**: Automatically revert to Git state (if enabled)
