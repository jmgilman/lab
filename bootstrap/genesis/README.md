# Genesis Bootstrap

Scripts and tools for bootstrapping the lab from scratch.

## Overview

The genesis bootstrap process provisions a USB drive with the boot media needed
to initialize the lab infrastructure. It installs Ventoy via VMware Fusion Pro,
downloads VyOS and Talos ISOs from e2 storage, and copies the VyOS gateway
configuration.

## Scripts

### provision-usb.py

Automates USB drive preparation for lab bootstrap. Since Ventoy only runs on
Linux/Windows and development machines are macOS, the script uses VMware Fusion
Pro and an Ubuntu cloud image with cloud-init to install Ventoy automatically.

**Usage:**

```bash
# Interactive mode (prompts for device)
./scripts/provision-usb.py

# Specify device directly
./scripts/provision-usb.py -d disk4

# Skip Ventoy installation (USB already has Ventoy)
./scripts/provision-usb.py -d disk4 --skip-ventoy

# Non-interactive mode
./scripts/provision-usb.py -d disk4 -y

# Show help
./scripts/provision-usb.py --help
```

**Options:**

| Option | Description |
|:-------|:------------|
| `-d, --device DEVICE` | USB device to provision (e.g., disk4) |
| `-s, --skip-download` | Skip ISO download, use cached files |
| `-v, --skip-ventoy` | Skip Ventoy installation |
| `-y, --yes` | Skip confirmation prompts |
| `-h, --help` | Show help message |

## Justfile

A `justfile` is provided for formatting and linting the genesis tooling.

From the repo root:

```bash
just -f bootstrap/genesis/justfile check
just -f bootstrap/genesis/justfile fmt
just -f bootstrap/genesis/justfile lint
just -f bootstrap/genesis/justfile clean
```

From `bootstrap/genesis/`:

```bash
just check
```

## Prerequisites

### uv

The script uses [uv](https://docs.astral.sh/uv/) for dependency management. Install it with:

```bash
curl -LsSf https://astral.sh/uv/install.sh | sh
```

The script automatically installs its dependencies (click, rich, boto3, httpx)
on first run.

### VMware Fusion Pro

Required for Ventoy installation on macOS. VMware Fusion Pro is now free for
personal use.

1. Download from: https://www.vmware.com/products/fusion.html
2. Install and run once to complete setup
3. The script uses the `vmrun` CLI for VM management

### sops

Required to decrypt the e2 storage credentials in `images/e2.sops.yaml`:

```bash
brew install sops
```

### qemu (ARM64 Macs only)

Required on Apple Silicon Macs to convert Ubuntu cloud images:

```bash
brew install qemu
```

This provides `qemu-img` which converts QCOW2 images to VMDK format for VMware.

### e2 credentials and image manifest

The script reads ISO paths from `images/images.yaml` and decrypts
`images/e2.sops.yaml` via `sops -d` to load e2 credentials. Ensure both files
exist and are up to date.

### USB Drive

- Minimum 8GB recommended
- Will be completely erased during provisioning

## How It Works

1. **Device selection**: lists external USB devices and prompts for selection
2. **ISO config**: resolves VyOS/Talos paths from `images/images.yaml`
3. **Credential load**: decrypts `images/e2.sops.yaml` to access e2 storage
4. **ISO download**: fetches ISOs from e2 storage with progress bars
5. **VM prep**: downloads Ubuntu cloud image and generates a VMX with USB passthrough
6. **Ventoy install**: cloud-init downloads Ventoy in the VM and installs it automatically
7. **File copy**: mounts the Ventoy partition and copies ISOs + `gateway.conf`
8. **Cleanup**: ejects the USB and removes transient VM artifacts

### VMware Fusion Approach

Since Ventoy does not run natively on macOS, the script:

1. Creates a VMX configuration with USB auto-connect based on VID:PID
2. Boots an Ubuntu cloud image with cloud-init to run the install script
3. Waits for the VM to shut down on successful Ventoy installation
4. Removes transient VM files while keeping cached disks for faster reruns

This approach was chosen because:
- VMware Fusion Pro is now free
- Reliable USB passthrough via VID:PID matching
- `vmrun` provides CLI control of VMs
- No manual steps required inside the VM

## Cache Directory

Downloaded files are cached at `~/.cache/lab-bootstrap/`:

```
~/.cache/lab-bootstrap/
├── ubuntu-noble-cloudimg-x86_64.ova
├── ubuntu-noble-cloudimg-arm64.img
├── isos/
│   ├── vyos-<version>.iso
│   └── talos-<version>-um760.iso
└── vms/
    └── ventoy-installer/
        ├── ubuntu-noble-cloudimg-<arch>.vmdk
        └── ubuntu-noble-cloudimg-<arch>.vmdk.meta.json
```

## Troubleshooting

### sops decryption fails

1. Ensure `sops` is installed (`brew install sops`)
2. Verify `images/e2.sops.yaml` exists and you have access to decrypt it

### USB device not detected by VM

1. Ensure USB is unmounted before VM starts
2. Check VID:PID detection - the script will prompt for manual entry if needed
3. Run `ioreg -r -c IOUSBHostDevice -l` to inspect USB devices

### Ventoy installation times out

1. Check that the VM is running in VMware Fusion
2. Ensure the USB device appears as `/dev/sdX` in the VM
3. Re-run with `--skip-ventoy` only if Ventoy is already installed

### ISO download fails

1. Verify `images/images.yaml` references valid objects
2. Confirm credentials in `images/e2.sops.yaml` are current
3. Use `--skip-download` to force cached ISOs

### Script fails to start

1. Ensure uv is installed: `which uv`
2. Try running directly: `uv run --script ./scripts/provision-usb.py --help`

## Related Documentation

- [Bootstrap Procedure](../../docs/architecture/appendices/B_bootstrap_procedure.md) - Full bootstrap runbook
- [VyOS GitOps](../../docs/architecture/09_design_decisions/003_vyos_gitops.md) - VyOS configuration management
- [images.yaml](../../images/images.yaml) - Image manifest for labctl sync
