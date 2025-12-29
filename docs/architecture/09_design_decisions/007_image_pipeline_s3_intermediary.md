# ADR 007: Image Pipeline S3 Intermediary Storage

**Status**: Proposed
**Date**: 2025-12-19

## Context

The lab requires machine images (Talos, VyOS, Harvester) to be available on the Synology NAS for PXE provisioning via Tinkerbell and for manual bootstrap. Images are primarily obtained via HTTP downloads from vendors (Talos Factory, Rancher, VyOS community builds).

We need a GitOps-friendly pipeline to:
- Declaratively define required images in Git
- Automate acquisition (download or build)
- Deliver images to the NAS for consumption

The challenge: GitHub Actions runners cannot directly access the NAS (it's behind the lab firewall).

## Options

### Option A: Direct NAS Upload via Tailscale

Use Tailscale to connect GitHub Actions runner to the lab network, then SCP/rsync images directly to NAS.

* **Pros**: Simpler architecture, fewer moving parts
* **Cons**: Large file transfers over Tailscale (slow); runner needs NAS credentials; NAS must accept SSH

### Option B: S3 Intermediary with Cloud Sync

Push images to iDrive e2 (S3-compatible) from GitHub Actions. Synology Cloud Sync pulls from e2 to local storage.

* **Pros**: Decoupled architecture; leverages existing NAS feature; no inbound connections to NAS
* **Cons**: Additional storage cost (minimal for image sizes); slight sync delay

### Option C: Self-Hosted Runner in Lab

Run GitHub Actions runner inside the lab with direct NAS access.

* **Pros**: Direct access, no tunneling
* **Cons**: Requires dedicated compute; single point of failure; must maintain runner

### Option D: GitHub Releases + NAS Pull

Store images as GitHub Release assets. Script on NAS polls for new releases.

* **Pros**: Uses existing infrastructure
* **Cons**: 2GB file size limit; not designed for large binaries; polling-based

## Decision

**Use Option B: S3 intermediary with iDrive e2 and Synology Cloud Sync.**

```
┌────────────────────────────────────────────────────────────────────────┐
│                          GitHub Actions                                 │
│  ┌──────────────────────────────────────────────────────────────────┐   │
│  │  Workflow: images-sync.yml                                       │   │
│  │                                                                   │   │
│  │  1. Build labctl CLI                                             │   │
│  │  2. Parse images/images.yaml                                     │   │
│  │  3. For each image:                                              │   │
│  │     - Download from vendor (HTTP)                                │   │
│  │     - Verify checksum                                            │   │
│  │     - Decompress if needed                                       │   │
│  │  4. Upload to iDrive e2                                          │   │
│  └──────────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────┬──────────────────────────────────────┘
                                  │ S3 API (HTTPS)
                    ┌─────────────▼─────────────┐
                    │       iDrive e2           │
                    │   (S3-compatible)         │
                    │                           │
                    │   lab-images/             │
                    │   ├── images/             │
                    │   │   ├── talos/          │
                    │   │   ├── vyos/           │
                    │   │   └── harvester/      │
                    │   └── metadata/           │
                    └─────────────┬─────────────┘
                                  │ Cloud Sync (pull)
                    ┌─────────────▼─────────────┐
                    │     Synology NAS          │
                    │                           │
                    │   /volume1/images/        │
                    │   ├── talos/              │
                    │   ├── vyos/               │
                    │   └── harvester/          │
                    └───────────────────────────┘
```

## Rationale

1. **Security**: No inbound connections to NAS required. Cloud Sync initiates outbound pulls only.

2. **Reliability**: S3 provides durable storage independent of NAS availability. If NAS is offline, images accumulate in e2 and sync when NAS returns.

3. **Existing Infrastructure**: Synology Cloud Sync is a built-in feature requiring no additional software.

4. **Performance**: Direct uploads to e2 are fast (public internet). Cloud Sync handles the final hop to NAS on the local network.

5. **Cost**: iDrive e2 offers 10GB free, with affordable pricing beyond. Image storage is typically < 20GB total.

6. **Decoupling**: The CI/CD pipeline doesn't need to know about NAS connectivity. It only needs S3 credentials.

## Implementation

### iDrive e2 Setup

1. Create iDrive e2 account
2. Create bucket: `lab-images`
3. Generate access keys:
   - **CI/CD key**: Read/Write access (for GitHub Actions)
   - **Cloud Sync key**: Read-only access (for NAS)

### Synology Cloud Sync Configuration

1. Install Cloud Sync package on NAS
2. Create sync task:
   - Provider: S3-compatible
   - Endpoint: iDrive e2 endpoint
   - Bucket: `lab-images`
   - Remote path: `images/`
   - Local path: `/volume1/images/`
   - Direction: **Download only**
   - Schedule: Continuous or every 5 minutes

### GitHub Secrets

| Secret | Purpose |
|:-------|:--------|
| `E2_ACCESS_KEY` | iDrive e2 access key ID |
| `E2_SECRET_KEY` | iDrive e2 secret access key |
| `E2_ENDPOINT` | iDrive e2 endpoint URL |

### Sync Delay Consideration

Cloud Sync introduces a delay (typically < 5 minutes) between upload and NAS availability. This is acceptable because:

- Image updates are infrequent (new OS releases)
- PXE provisioning is not time-critical
- The sync delay is bounded and predictable

For urgent updates, Cloud Sync can be triggered manually via Synology UI.

## Consequences

- **Additional Service Dependency**: iDrive e2 becomes a dependency (mitigated by its reliability and low cost)
- **Sync Configuration Not GitOps**: Cloud Sync setup is manual on NAS (documented in appendix for DR)
- **Separate Credential Management**: e2 credentials managed outside of lab's normal secret management (GitHub Secrets + NAS Cloud Sync config)

## Alternatives Considered

- **Backblaze B2**: Similar to e2, but less S3-compatible; Cloud Sync support varies
- **AWS S3**: More expensive; overkill for this use case
- **MinIO in Lab**: Would require lab compute for storage; adds complexity

## References

- [iDrive e2 Documentation](https://www.idrive.com/e2/)
- [Synology Cloud Sync](https://www.synology.com/en-us/dsm/feature/cloud_sync)
- Design Document: `docs/design/image-pipeline.md`
