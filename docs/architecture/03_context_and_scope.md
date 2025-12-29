# 03. Context and Scope

## Business Context
**Goal**: To provide a realistic, isolated, and scalable infrastructure environment for Platform Engineering research and personal services hosting.

**Primary User**: The Administrator (Josh).
**Interaction Models**:
1.  **GitOps Workflow**: User pushes code to GitHub -> Argo CD synchronizes state.
2.  **Direct Access**: User accesses hosted web services (Grafana, Argo UI, Applications) via browser on the Home Network.
3.  **Bootstrap**: User manually initiates the "Genesis" sequence (rare event).

## Technical Context
The Lab System is designed as a "Black Box" that sits adjacent to the Home Network. It interacts with the outside world through strictly defined interfaces.

### System Context Diagram

```mermaid
C4Context
    title System Context Diagram (Homelab)

    Person(user, "Administrator", "Manages infrastructure via Git and accesses services via Web.")
    System(lab, "Homelab Platform", "The bare-metal Kubernetes cloud environment.")

    System_Ext(github, "GitHub", "Hosts source code, config, and pipelines.")
    System_Ext(home_net, "Home Network", "Existing trusted network (Workstations, IoT).")
    System_Ext(internet, "Internet", "Upstream connectivity.")

    Rel(user, github, "Pushes Config / Code", "Git")
    Rel(user, home_net, "Uses Workstation", "LAN")

    Rel(lab, github, "Pulls State", "HTTPS")
    Rel(lab, internet, "Downloads Images / Updates", "HTTPS")

    Rel(home_net, lab, "Accesses Services (Web/SSH)", "HTTPS / TCP (Routed)")
    Rel(lab, home_net, "Responds to Requests", "TCP")
```

### External Interfaces

| Interface | Type | Description |
| :--- | :--- | :--- |
| **Home -> Lab (Data)** | **IPv4 Routing** | Traffic from Home Workstations (192.168.1.0/24) is routed via the Transit Link (10.0.0.0/30) to Lab VIPs (10.10.x.x). **Firewalled ALLOW**. |
| **Lab -> Home (Data)** | **IPv4 Routing** | Lab nodes attempting to initiate connections to Home devices. **Firewalled DROP** (except specific replies). |
| **Lab -> Internet** | **NAT** | Lab nodes access the internet via the Gateway (VP6630) which performs NAT. |
| **GitOps Sync** | **HTTPS** | The Lab (Argo CD) polls GitHub repositories to synchronize state. |

### Scope Definition

| Zone | Status | Description |
| :--- | :--- | :--- |
| **Infrastructure** | **In Scope** | Hardware, Provisioning (Tinkerbell), Hypervisor (Harvester), Kubernetes (Talos). |
| **Platform Services** | **In Scope** | Identity (Zitadel), Secrets (OpenBAO), Observability, Ingress, Cert-Manager. |
| **Home LAN** | **Out of Scope** | Wi-Fi Access Points, Smart Home Hubs, IoT devices, Corporate Laptops. |
