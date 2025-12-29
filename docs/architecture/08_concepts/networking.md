# 08. Concepts - Networking

## Overview
The lab network implements a **segmented VLAN architecture** with physical separation of control planes. This design isolates provisioning, management, and workload traffic while providing high-bandwidth connectivity for storage replication.

> [!IMPORTANT]
> **Design Decision**: This architecture implements **LACP Bonding** for the Data Plane (See [ADR 002](../09_design_decisions/002_networking_topology.md)). All MS-02 nodes use a 20Gbps bonded uplink (2x 10GbE).

---

## Physical Topology

### Core Infrastructure

| Component | Role | Connectivity |
|:---|:---|:---|
| **VP6630** | VyOS Gateway / Router-on-a-Stick | Inter-VLAN routing, NAT, BGP |
| **Mikrotik Switch** | 10GbE SFP+ switching | LACP trunks to Harvester nodes |
| **MS-02 (x3)** | Harvester HCI nodes | Split Plane (see below) |
| **UM760** | Platform Control Plane | Hybrid Trunk |

### Split Plane Architecture (MS-02)

The MS-02 nodes use a **Split Plane** design to accommodate hardware constraints (vPro/AMT only works on 2.5GbE ports):

```
MS-02 Node
├── 2.5GbE Port ──▶ VyOS Gateway ──▶ VLAN 20 (OOB/Provisioning)
│   └── vPro/AMT, PXE boot
│
└── 2x 10GbE SFP+ ──▶ Mikrotik Switch ──▶ LACP Bond (Data Plane)
    └── VLANs 10, 30, 40, 50, 60
```

### Interface Configuration

| Node | Interface | Config | VLANs | Purpose |
|:---|:---|:---|:---|:---|
| **MS-02** | `bond0` (2x 10G) | LACP Trunk | 10, 30, 40, 50, 60 | Data Plane |
| **MS-02** | `enp*` (2.5G) | Access | 20 (Native) | OOB Plane |
| **UM760** | `eth0` (2.5G) | Hybrid Trunk | 20 (Native), 30, 40, 50 | Unified Plane |

---

## VLAN Architecture

| ID | Name | Subnet | DHCP Source | Purpose |
|:---|:---|:---|:---|:---|
| **10** | `LAB_MGMT` | `10.10.10.0/24` | VyOS | Infrastructure management (Harvester hosts, switch) |
| **20** | `LAB_PROV` | `10.10.20.0/24` | Tinkerbell | Provisioning (PXE, TFTP) — "dirty" segment |
| **30** | `LAB_PLATFORM` | `10.10.30.0/24` | VyOS | Platform Cluster traffic |
| **40** | `LAB_CLUSTER` | `10.10.40.0/24` | VyOS | Downstream Cluster traffic |
| **50** | `LAB_SERVICE` | `10.10.50.0/24` | BGP (dynamic) | Service VIPs (LoadBalancer endpoints) |
| **60** | `LAB_STORAGE` | `10.10.60.0/24` | Static | Storage replication (L2 only, non-routed) |

### VLAN Diagram

```
                    ┌─────────────────────────────────────┐
                    │            VyOS Gateway             │
                    │  Router-on-a-Stick (All VLANs)      │
                    └──────────────────┬──────────────────┘
                                       │
          ┌────────────────────────────┼────────────────────────────┐
          │                            │                            │
     VLAN 10                      VLAN 30/40                   VLAN 50
   Management                   Cluster Traffic              Service VIPs
          │                            │                       (BGP)
          ▼                            ▼
   ┌─────────────┐            ┌─────────────────┐
   │  Harvester  │            │  Platform +     │
   │   Hosts     │            │  Downstream VMs │
   └─────────────┘            └─────────────────┘
```

---

## IP Addressing Scheme

Each VLAN follows a consistent addressing convention:

| Range | Usage | Example |
|:---|:---|:---|
| `.1` | Gateway (VyOS) | `10.10.30.1` |
| `.2 - .9` | Infrastructure (static) | Switch, APs |
| `.10 - .49` | Critical reservations | Harvester VIP, NAS |
| `.50 - .200` | DHCP pool | VM nodes |
| `.201 - .254` | Reserved / spare | Future use |

---

## External Integration

### Home ↔ Lab Connectivity

The lab is logically isolated from the home network but accessible via a **dedicated /30 Transit Link**:

| Component | Address | Role |
|:---|:---|:---|
| **CCR2004** | `10.0.0.1/30` | Home router (transit side) |
| **VP6630** | `10.0.0.2/30` | Lab gateway (transit side) |
| **Home Network** | `192.168.1.0/24` | Home LAN (routed via transit) |

| Direction | Mechanism | Policy |
|:---|:---|:---|
| **Home → Lab** | Static route on CCR2004 | `10.10.0.0/16` via `10.0.0.2` |
| **Lab → Home** | Static route on VP6630 + Firewall | `192.168.1.0/24` via `10.0.0.1` (HOME_NETWORK group allowed) |
| **Lab → Internet** | NAT masquerade via VP6630 | Outbound via transit link |

```
┌─────────────────┐                           ┌─────────────────┐
│   Home Network  │                           │   Lab Network   │
│  192.168.1.0/24 │                           │   10.10.0.0/16  │
└────────┬────────┘                           └────────┬────────┘
         │                                             │
    CCR2004                                        VP6630
    10.0.0.1 ◀──── Transit Link (10.0.0.0/30) ────▶ 10.0.0.2
   (Static Route)                               (NAT/Firewall)
```

### Firewall Policy
- **Inbound (Home → Lab)**: ALLOW (access services, UIs)
- **Outbound (Lab → Home)**: DROP (lab cannot scan/access home devices)
- **Lab → Internet**: NAT via VP6630
