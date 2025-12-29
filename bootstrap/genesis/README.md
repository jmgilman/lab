# Genesis Bootstrap

Scripts and tools for bootstrapping the lab from scratch.

## Overview

The genesis bootstrap process provisions a USB drive with the necessary boot media to initialize the lab infrastructure. This includes:

- **Ventoy** - Multi-boot USB bootloader
- **VyOS Stream ISO** - For installing the lab gateway router
- **Talos ISO** - With embedded machine configuration for UM760 bootstrap
- **gateway.conf** - VyOS configuration file

## Scripts

### provision-usb.py

Automates USB drive preparation for lab bootstrap. Since Ventoy only runs on Linux/Windows and development machines are macOS, this script uses VMware Fusion Pro as an intermediary to run Ventoy installation.

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

## Prerequisites

### uv

The script uses [uv](https://docs.astral.sh/uv/) for dependency management. Install it with:

```bash
curl -LsSf https://astral.sh/uv/install.sh | sh
```

The script automatically installs its dependencies (click, rich, boto3, httpx) on first run.

### VMware Fusion Pro

Required for Ventoy installation on macOS. VMware Fusion Pro is now free for personal use.

1. Download from: https://www.vmware.com/products/fusion.html
2. Install and run once to complete setup
3. The script uses `vmrun` CLI for VM management

### qemu (ARM64 Macs only)

Required on Apple Silicon Macs to convert Ubuntu cloud images:

```bash
brew install qemu
```

This provides `qemu-img` which converts QCOW2 images to VMDK format for VMware.

### AWS Credentials

Required for downloading ISOs from e2 storage. Configure with:

```bash
# Option 1: AWS config file
aws configure
# Set: access key, secret key
# Endpoint is configured in the script

# Option 2: Environment variables
export AWS_ACCESS_KEY_ID=your_access_key
export AWS_SECRET_ACCESS_KEY=your_secret_key
```

### USB Drive

- Minimum 8GB recommended
- Will be completely erased during provisioning

## How It Works

1. **Device Selection**: Lists external USB devices and prompts for selection
2. **ISO Download**: Fetches ISOs from iDrive e2 storage with progress bars
3. **VM Creation**: Creates an ephemeral Alpine Linux VM with USB passthrough
4. **Ventoy Installation**: User runs Ventoy installer inside the VM (manual step)
5. **File Copy**: Copies ISOs and gateway.conf to the Ventoy partition
6. **Cleanup**: Removes VM files and safely ejects USB

### VMware Fusion Approach

Since Ventoy does not run natively on macOS, the script:

1. Creates a VMX configuration with USB auto-connect based on VID:PID
2. Boots Alpine Linux from ISO (lightweight, fast boot)
3. Attaches a second ISO containing Ventoy and an install script
4. User runs the installation manually in the VM console
5. VM is shut down and deleted after installation

This approach was chosen because:
- VMware Fusion Pro is now free
- Reliable USB passthrough via VID:PID matching
- `vmrun` provides CLI control of VMs
- No modification to the USB needed before passthrough

## Cache Directory

Downloaded files are cached at `~/.cache/lab-bootstrap/`:

```
~/.cache/lab-bootstrap/
├── alpine-virt-3.21.iso      # Alpine Linux boot ISO
├── ventoy-1.0.99.tar.gz      # Ventoy installer
├── isos/
│   ├── vyos-2025.11-generic-amd64.iso
│   └── talos-1.12.0-um760.iso
└── vms/                       # Ephemeral VM files (cleaned up)
```

## Troubleshooting

### USB device not detected by VM

1. Ensure USB is unmounted before VM starts
2. Check VID:PID detection - script will prompt for manual entry if needed
3. Run `ioreg -r -c IOUSBHostDevice -l` to inspect USB devices

### Ventoy installation fails

1. Check that USB device appears as `/dev/sdb` or similar in the VM
2. Run `lsblk` in the VM to verify device detection
3. Check `/sys/block/sdX/removable` shows `1`

### S3 download fails

1. Verify AWS credentials are configured
2. Test manually: `aws s3 ls s3://gilmanlabimages/ --endpoint-url https://t7h4.c13.e2-3.dev`
3. Use `--skip-download` to use cached ISOs

### Script fails to start

1. Ensure uv is installed: `which uv`
2. Try running directly: `uv run --script ./scripts/provision-usb.py --help`

## Related Documentation

- [Bootstrap Procedure](../../docs/architecture/appendices/B_bootstrap_procedure.md) - Full bootstrap runbook
- [VyOS GitOps](../../docs/architecture/09_design_decisions/003_vyos_gitops.md) - VyOS configuration management
- [images.yaml](../../images/images.yaml) - Image manifest for labctl sync
