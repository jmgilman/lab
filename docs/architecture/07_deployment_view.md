# 07. Deployment View

This section describes the physical and virtual infrastructure topology — how hardware is organized, connected, and where software components run.

---

## Physical Topology

### Network Diagram

```
                                    ┌─────────────────────────────┐
                                    │        Internet             │
                                    └──────────────┬──────────────┘
                                                   │
                                    ┌──────────────▼──────────────┐
                                    │     CCR2004 (Home Router)   │
                                    │  Home LAN: 192.168.1.1      │
                                    │  Transit:  10.0.0.1/30      │
                                    └──────────────┬──────────────┘
                                                   │ Transit Link (10.0.0.0/30)
                                    ┌──────────────▼──────────────┐
                                    │     VP6630 (VyOS Gateway)   │
                                    │  Lab Router / Firewall      │
                                    │  Transit:  10.0.0.2/30      │
                                    │  Lab VLANs: 10.10.x.1       │
                                    └──┬─────────────────────┬────┘
                                       │                     │
                          2.5G (OOB)   │                     │  Trunk to Switch
                    ┌──────────────────┘                     │
                    │                                        │
                    ▼                                        ▼
    ┌───────────────────────────┐          ┌────────────────────────────────┐
    │   MS-02 (x3) - 2.5GbE     │          │   Mikrotik Switch (10G SFP+)   │
    │   VLAN 20 (Provisioning)  │          │   LACP Trunks to MS-02s        │
    │   vPro/AMT, PXE           │          │   VLAN 10,30,40,50,60          │
    └───────────────────────────┘          └────────────────────────────────┘
                                                        │
                    ┌───────────────────────────────────┼───────────────────┐
                    │                                   │                   │
                    ▼                                   ▼                   ▼
          ┌─────────────────┐              ┌─────────────────┐    ┌─────────────────┐
          │    MS-02 #1     │              │    MS-02 #2     │    │    MS-02 #3     │
          │   (Harvester)   │              │   (Harvester)   │    │   (Harvester)   │
          │  bond0: 2x10G   │              │  bond0: 2x10G   │    │  bond0: 2x10G   │
          └─────────────────┘              └─────────────────┘    └─────────────────┘
                    │                                   │                   │
                    └───────────────────────────────────┴───────────────────┘
                                           │
                              Longhorn Replication (VLAN 60)
```

### Infrastructure Elements

| Node | Role | Hardware | Network |
|:---|:---|:---|:---|
| **MS-02 (x3)** | Harvester HCI | i9-12900H, 64GB RAM, NVMe | Split: 2.5G (OOB) + 2x10G LACP (Data) |
| **UM760** | Platform Control Plane | Ryzen 7, 32GB RAM | Hybrid: 2.5G Trunk (VLAN 20 native, 30 tagged) |
| **Synology NAS** | NFS / Backup / Bootstrap | DiskStation DS923+ | 10G SFP+ to Switch |
| **VP6630** | Gateway Router | VyOS | 4x 2.5G (LAN segments), 10G (future WAN) |
| **Mikrotik** | Lab Switch | CRS310-8G+2S+IN | 8x 10G SFP+ |

---

## VLAN Architecture

| VLAN | Name | Subnet | Purpose | Connected Nodes |
|:---|:---|:---|:---|:---|
| **10** | `LAB_MGMT` | `10.10.10.0/24` | Infrastructure management | Harvester hosts, Switch |
| **20** | `LAB_PROV` | `10.10.20.0/24` | Provisioning (PXE/DHCP) | MS-02 (2.5G), UM760 (native) |
| **30** | `LAB_PLATFORM` | `10.10.30.0/24` | Platform Cluster | UM760, Platform VMs |
| **40** | `LAB_CLUSTER` | `10.10.40.0/24` | Downstream Clusters | Workload VMs |
| **50** | `LAB_SERVICE` | `10.10.50.0/24` | Service VIPs (BGP) | LoadBalancer endpoints |
| **60** | `LAB_STORAGE` | `10.10.60.0/24` | Storage replication | Longhorn (L2 only) |

---

## Software Deployment

### Node OS Mapping

| Node | Operating System | Deployment Method |
|:---|:---|:---|
| **VP6630** | VyOS | Manual install from Stream ISO |
| **MS-02 (x3)** | Harvester (Elemental OS) | Tinkerbell PXE |
| **UM760** | Talos Linux | Tinkerbell PXE |
| **Platform VMs (x2)** | Talos Linux | CAPI + Harvester |
| **Downstream VMs** | Talos Linux | CAPI + Harvester |
| **Synology NAS** | DSM | Factory |

### Service Placement

```
┌─────────────────────────────────────────────────────────────────────────┐
│                           Platform Cluster                              │
│  ┌─────────────────────────────────────────────────────────────────┐    │
│  │  Argo CD │ Zitadel │ OpenBAO │ CAPI │ Tinkerbell │ Prometheus  │    │
│  └─────────────────────────────────────────────────────────────────┘    │
│        Runs on: UM760 (physical) + 2x Harvester VMs                     │
│        Manages: Itself, Harvester, Downstream Clusters (hub-and-spoke)  │
└─────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────┐
│                          Harvester HCI                                  │
│  ┌─────────────────────────────────────────────────────────────────┐    │
│  │  KubeVirt │ Longhorn │ Harvester UI (Embedded Rancher)         │    │
│  └─────────────────────────────────────────────────────────────────┘    │
│        Runs on: MS-02 x3 (bare metal)                                   │
│        Managed by: Argo CD (registered as cluster via kubeconfig)       │
│        Hosts: Platform VMs (CP-2, CP-3), Tenant cluster VMs             │
└─────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────┐
│                        Downstream Clusters                              │
│  ┌─────────────────────────────────────────────────────────────────┐    │
│  │  Cilium │ Vault Agent │ Application Workloads                  │    │
│  └─────────────────────────────────────────────────────────────────┘    │
│        Runs on: Harvester VMs (ephemeral)                               │
│        Managed by: Argo CD (auto-registered via TenantCluster XR)       │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## Harvester as a Managed Cluster

Harvester is unique in this architecture — it's a Kubernetes cluster, but serves as the HCI (Hyperconverged Infrastructure) layer rather than a workload cluster.

### Why Harvester is Managed by Argo CD

| Aspect | Rationale |
|:---|:---|
| **Configuration as Code** | Harvester networks, images, and VMs are defined in Git as CRDs |
| **Bootstrap VMs** | Platform CP-2 and CP-3 VMs must be created before CAPI is available |
| **Standalone VMs** | Non-containerized workloads (gaming, file servers) live permanently on Harvester |
| **Hub-and-Spoke** | Single Argo CD instance manages all clusters uniformly |

### Cluster Registration

Harvester is registered with Argo CD during Phase 3 of bootstrap:

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
  server: https://harvester.lab.local:6443
  config: |
    {
      "tlsClientConfig": {
        "caData": "...",
        "certData": "...",
        "keyData": "..."
      }
    }
```

### What Argo CD Manages on Harvester

```
clusters/harvester/
├── config/
│   ├── networks/           # ClusterNetwork / VlanConfig CRDs
│   └── images/             # VirtualMachineImage CRDs (Talos, etc.)
└── vms/
    ├── platform/           # Platform CP-2, CP-3 (bootstrap phase)
    └── standalone/         # Non-container workloads (gaming, NAS, etc.)
```

**Note**: Tenant cluster VMs are NOT managed via `clusters/harvester/`. They are created dynamically by CAPI when a TenantCluster XR is applied.

---

## External Dependencies

| Dependency | Type | Purpose |
|:---|:---|:---|
| **GitHub** | SaaS | Git repository, GitOps source of truth |
| **Internet** | Connectivity | Image pulls, updates, external APIs |
| **Home Router (CCR2004)** | Physical | Upstream routing, transit to lab |

> [!NOTE]
> The lab is designed to operate in **degraded mode** without internet access. Cached images and local registries enable continued operation; only GitOps sync and external pulls are affected.
