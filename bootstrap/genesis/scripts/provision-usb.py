#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.11"
# dependencies = [
#     "click>=8.1",
#     "rich>=13.0",
#     "boto3>=1.34",
#     "httpx>=0.27",
#     "pyyaml>=6.0",
# ]
# ///
"""
Provision a USB drive with Ventoy and lab bootstrap media.

This script automates USB preparation for lab bootstrap by:
1. Installing Ventoy on the USB drive via VMware Fusion Pro (Ubuntu cloud VM)
2. Downloading VyOS and Talos ISOs from iDrive e2 storage
3. Copying the VyOS gateway configuration file

The script uses Ubuntu cloud images with cloud-init for fully automated
Ventoy installation - no manual intervention required.
"""

from __future__ import annotations

import json
import re
import shutil
import subprocess
import tarfile
import time
from dataclasses import dataclass
from pathlib import Path

import yaml
import boto3
import click
import httpx
from rich.console import Console as RichConsole
from rich.panel import Panel
from rich.progress import (
    BarColumn,
    DownloadColumn,
    Progress,
    TextColumn,
    TimeRemainingColumn,
    TransferSpeedColumn,
)
from rich.prompt import Confirm, Prompt
from rich.table import Table

# =============================================================================
# Configuration
# =============================================================================

SCRIPT_DIR = Path(__file__).parent.resolve()
REPO_ROOT = SCRIPT_DIR.parent.parent.parent

# VMware Fusion paths
VMWARE_APP = Path("/Applications/VMware Fusion.app")
VMRUN = VMWARE_APP / "Contents/Library/vmrun"
OVFTOOL = VMWARE_APP / "Contents/Library/VMware OVF Tool/ovftool"

# Ventoy configuration
VENTOY_VERSION = "1.0.99"

# Ubuntu cloud image configuration
# Note: OVA is only available for amd64, ARM64 requires .img conversion
UBUNTU_VERSION = "noble"  # 24.04 LTS
UBUNTU_CLOUD_BASE = f"https://cloud-images.ubuntu.com/{UBUNTU_VERSION}/current"
UBUNTU_CLOUD_IMAGES = {
    "x86_64": {
        "url": f"{UBUNTU_CLOUD_BASE}/{UBUNTU_VERSION}-server-cloudimg-amd64.ova",
        "format": "ova",
    },
    "arm64": {
        "url": f"{UBUNTU_CLOUD_BASE}/{UBUNTU_VERSION}-server-cloudimg-arm64.img",
        "format": "qcow2",  # Ubuntu .img files are QCOW2 format
    },
}

# ISO paths in e2 storage (bucket/endpoint loaded from SOPS credentials)
# Note: labctl stores images with an "images/" prefix
# These are now loaded dynamically from images.yaml
IMAGES_MANIFEST = REPO_ROOT / "images/images.yaml"

# Local cache directory
CACHE_DIR = Path.home() / ".cache/lab-bootstrap"
ISO_CACHE_DIR = CACHE_DIR / "isos"

# VyOS configuration file
VYOS_CONFIG = REPO_ROOT / "infrastructure/network/vyos/configs/gateway.conf"

# e2 credentials file (SOPS encrypted)
E2_CREDENTIALS_FILE = REPO_ROOT / "images/e2.sops.yaml"


@dataclass
class E2Credentials:
    """e2 storage credentials."""

    access_key: str
    secret_key: str
    endpoint: str
    bucket: str


@dataclass
class USBDevice:
    """USB device information."""

    identifier: str
    name: str
    size: str


def get_host_arch() -> str:
    """Get the host machine architecture."""
    import platform

    machine = platform.machine()
    if machine in ("arm64", "aarch64"):
        return "arm64"
    return "x86_64"


def load_iso_config() -> tuple[str, str]:
    """Load ISO paths from images.yaml."""
    if not IMAGES_MANIFEST.exists():
        raise FileNotFoundError(f"Images manifest not found: {IMAGES_MANIFEST}")

    with IMAGES_MANIFEST.open() as f:
        data = yaml.safe_load(f)

    vyos_path = ""
    talos_path = ""

    for image in data.get("spec", {}).get("images", []):
        if image["name"] == "vyos-stream":
            vyos_path = f"images/{image['destination']}"
        elif image["name"] == "talos-um760":
            talos_path = f"images/{image['destination']}"

    if not vyos_path:
        raise ValueError("Could not find 'vyos-stream' image in manifest")
    if not talos_path:
        raise ValueError("Could not find 'talos-um760' image in manifest")

    return vyos_path, talos_path


def load_e2_credentials() -> E2Credentials:
    """Load e2 credentials from SOPS-encrypted file."""
    if not E2_CREDENTIALS_FILE.exists():
        raise FileNotFoundError(f"e2 credentials file not found: {E2_CREDENTIALS_FILE}")

    # Check if sops is available
    if not shutil.which("sops"):
        raise RuntimeError("sops is not installed. Install with: brew install sops")

    # Decrypt the file using sops
    result = subprocess.run(
        ["sops", "-d", str(E2_CREDENTIALS_FILE)],
        capture_output=True,
        text=True,
    )

    if result.returncode != 0:
        raise RuntimeError(f"Failed to decrypt e2 credentials: {result.stderr}")

    # Parse the YAML
    data = yaml.safe_load(result.stdout)

    try:
        return E2Credentials(
            access_key=data["access_key"],
            secret_key=data["secret_key"],
            endpoint=data["endpoint"],
            bucket=data["bucket"],
        )
    except KeyError as e:
        raise ValueError(f"Missing required key in e2 credentials: {e}") from e


# =============================================================================
# Console Output
# =============================================================================


class Console:
    """Pretty console output using rich."""

    def __init__(self) -> None:
        self.console = RichConsole()

    def info(self, message: str) -> None:
        self.console.print(f"[blue][INFO][/blue] {message}")

    def success(self, message: str) -> None:
        self.console.print(f"[green][OK][/green] {message}")

    def warn(self, message: str) -> None:
        self.console.print(f"[yellow][WARN][/yellow] {message}")

    def error(self, message: str) -> None:
        self.console.print(f"[red][ERROR][/red] {message}")

    def banner(self, title: str) -> None:
        self.console.print(Panel(title, style="bold blue"))

    def confirm(self, message: str, default: bool = False) -> bool:
        return Confirm.ask(message, default=default)

    def prompt(self, message: str) -> str:
        return Prompt.ask(message)

    def table(self, title: str, columns: list[str], rows: list[list[str]]) -> None:
        table = Table(title=title)
        for col in columns:
            table.add_column(col)
        for row in rows:
            table.add_row(*row)
        self.console.print(table)

    def progress(self) -> Progress:
        return Progress(
            TextColumn("[bold blue]{task.description}"),
            BarColumn(),
            DownloadColumn(),
            TransferSpeedColumn(),
            TimeRemainingColumn(),
            console=self.console,
        )


console = Console()


# =============================================================================
# USB Device Management
# =============================================================================


class USBDeviceManager:
    """Manage USB device detection and operations."""

    def list_external_devices(self) -> list[USBDevice]:
        """List external USB devices."""
        result = subprocess.run(
            ["diskutil", "list", "external", "physical"],
            capture_output=True,
            text=True,
        )

        if result.returncode != 0 or not result.stdout.strip():
            return []

        devices = []
        for line in result.stdout.splitlines():
            # Match lines like "/dev/disk4 (external, physical):"
            match = re.match(r"(/dev/disk\d+)\s+\(external", line)
            if match:
                device_path = match.group(1)
                identifier = device_path.replace("/dev/", "")
                info = self.get_device_info(identifier)
                if info:
                    devices.append(info)

        return devices

    def get_device_info(self, identifier: str) -> USBDevice | None:
        """Get detailed information about a device."""
        result = subprocess.run(
            ["diskutil", "info", identifier],
            capture_output=True,
            text=True,
        )

        if result.returncode != 0:
            return None

        name = "Unknown"
        size = "Unknown"

        for line in result.stdout.splitlines():
            if "Device / Media Name:" in line:
                name = line.split(":", 1)[1].strip()
            elif "Disk Size:" in line:
                # Extract human-readable size
                size = line.split(":", 1)[1].strip()
                if "(" in size:
                    size = size.split("(")[0].strip()

        return USBDevice(identifier=identifier, name=name, size=size)

    def extract_vid_pid(self, identifier: str) -> str | None:
        """Extract USB VID:PID from ioreg.

        Strategy: Get the device name from diskutil, then find matching
        USB device in ioreg to get VID:PID.
        """
        # Get the device name from diskutil
        device_info = self.get_device_info(identifier)
        if not device_info:
            return None

        device_name = device_info.name

        # Query USB devices from ioreg
        result = subprocess.run(
            ["ioreg", "-r", "-c", "IOUSBHostDevice", "-l"],
            capture_output=True,
            text=True,
        )

        if result.returncode != 0:
            return None

        # Parse ioreg output to find the USB device with matching name
        # We look for blocks containing the device name and extract VID/PID
        output = result.stdout

        # Split into device blocks (each starts with +-o)
        blocks = re.split(r"\+-o ", output)

        for block in blocks:
            # Check if this block contains our device name
            if (
                f'"USB Product Name" = "{device_name}"' in block
                or f'"kUSBProductString" = "{device_name}"' in block
            ):
                # Extract VID and PID from this block
                vid_match = re.search(r'"idVendor"\s*=\s*(\d+)', block)
                pid_match = re.search(r'"idProduct"\s*=\s*(\d+)', block)

                if vid_match and pid_match:
                    vid = int(vid_match.group(1))
                    pid = int(pid_match.group(1))
                    return f"{vid:04x}:{pid:04x}"

        return None

    def unmount(self, identifier: str) -> bool:
        """Unmount a disk."""
        result = subprocess.run(
            ["diskutil", "unmountDisk", f"/dev/{identifier}"],
            capture_output=True,
            text=True,
        )
        return result.returncode == 0

    def eject(self, identifier: str) -> bool:
        """Eject a disk."""
        result = subprocess.run(
            ["diskutil", "eject", f"/dev/{identifier}"],
            capture_output=True,
            text=True,
        )
        return result.returncode == 0

    def mount_partition(self, identifier: str, partition: int = 1) -> Path | None:
        """Mount a partition and return the mount point."""
        partition_id = f"{identifier}s{partition}"
        result = subprocess.run(
            ["diskutil", "mount", partition_id],
            capture_output=True,
            text=True,
        )

        if result.returncode != 0:
            return None

        # Extract mount point from output or query it
        info_result = subprocess.run(
            ["diskutil", "info", partition_id],
            capture_output=True,
            text=True,
        )

        for line in info_result.stdout.splitlines():
            if "Mount Point:" in line:
                mount_point = line.split(":", 1)[1].strip()
                if mount_point and Path(mount_point).exists():
                    return Path(mount_point)

        return None

    def wait_for_partition(
        self, identifier: str, partition: int = 1, timeout: int = 30
    ) -> bool:
        """Wait for a partition to appear."""
        partition_id = f"{identifier}s{partition}"
        for _ in range(timeout):
            result = subprocess.run(
                ["diskutil", "info", partition_id],
                capture_output=True,
                text=True,
            )
            if result.returncode == 0:
                return True
            time.sleep(1)
        return False


# =============================================================================
# Download Manager
# =============================================================================


class DownloadManager:
    """Manage file downloads with progress."""

    def __init__(self, credentials: E2Credentials | None = None) -> None:
        CACHE_DIR.mkdir(parents=True, exist_ok=True)
        ISO_CACHE_DIR.mkdir(parents=True, exist_ok=True)
        self.credentials = credentials

        # Load ISO paths dynamically
        try:
            self.vyos_path, self.talos_path = load_iso_config()
        except Exception as e:
            # Only fail if we actually need to download them and they aren't cached
            # But we can't check cache meaningfully without the path, so warn
            console.warn(f"Failed to load ISO config: {e}")
            self.vyos_path = ""
            self.talos_path = ""

    def download_http(self, url: str, dest: Path, description: str) -> bool:
        """Download a file via HTTP with progress."""
        if dest.exists():
            console.info(f"Using cached: {dest.name}")
            return True

        console.info(f"Downloading {description}...")

        try:
            with httpx.stream("GET", url, follow_redirects=True) as response:
                response.raise_for_status()
                total = int(response.headers.get("content-length", 0))

                with (
                    console.progress() as progress,
                    dest.open("wb") as f,
                ):
                    task = progress.add_task(description, total=total)
                    for chunk in response.iter_bytes(chunk_size=8192):
                        f.write(chunk)
                        progress.update(task, advance=len(chunk))

            console.success(f"Downloaded: {dest.name}")
            return True
        except httpx.HTTPError as e:
            console.error(f"Download failed: {e}")
            if dest.exists():
                dest.unlink()
            return False

    def download_s3(self, key: str, dest: Path, description: str) -> bool:
        """Download a file from S3 with progress."""
        if dest.exists():
            console.info(f"Using cached: {dest.name}")
            return True

        if not self.credentials:
            console.error("e2 credentials not configured")
            return False

        console.info(f"Downloading {description} from e2 storage...")

        try:
            s3 = boto3.client(
                "s3",
                endpoint_url=self.credentials.endpoint,
                aws_access_key_id=self.credentials.access_key,
                aws_secret_access_key=self.credentials.secret_key,
            )

            # Get file size
            head = s3.head_object(Bucket=self.credentials.bucket, Key=key)
            total = head["ContentLength"]

            with console.progress() as progress:
                task = progress.add_task(description, total=total)

                def callback(bytes_transferred: int) -> None:
                    progress.update(task, advance=bytes_transferred)

                s3.download_file(
                    self.credentials.bucket, key, str(dest), Callback=callback
                )

            console.success(f"Downloaded: {dest.name}")
            return True
        except Exception as e:
            console.error(f"S3 download failed: {e}")
            if dest.exists():
                dest.unlink()
            return False

    def download_ubuntu_image(self) -> tuple[Path, str] | None:
        """Download Ubuntu cloud image for the host architecture.

        Returns:
            Tuple of (path, format) where format is 'ova' or 'qcow2',
            or None if download failed.
        """
        arch = get_host_arch()
        image_info = UBUNTU_CLOUD_IMAGES[arch]
        url = image_info["url"]
        fmt = image_info["format"]

        ext = "ova" if fmt == "ova" else "img"
        dest = CACHE_DIR / f"ubuntu-{UBUNTU_VERSION}-cloudimg-{arch}.{ext}"

        if self.download_http(url, dest, f"Ubuntu Cloud Image ({arch})"):
            return dest, fmt
        return None

    def download_vyos_iso(self) -> Path | None:
        """Download VyOS ISO from e2."""
        if not self.vyos_path:
            return None
        dest = ISO_CACHE_DIR / Path(self.vyos_path).name
        if self.download_s3(self.vyos_path, dest, "VyOS ISO"):
            return dest
        return None

    def download_talos_iso(self) -> Path | None:
        """Download Talos ISO from e2."""
        if not self.talos_path:
            return None
        dest = ISO_CACHE_DIR / Path(self.talos_path).name
        if self.download_s3(self.talos_path, dest, "Talos ISO"):
            return dest
        return None


# =============================================================================
# VMware Manager
# =============================================================================


class VMwareManager:
    """Manage VMware Fusion VMs for Ventoy installation."""

    def __init__(self) -> None:
        self.vm_name = "ventoy-installer"
        self.vm_dir = CACHE_DIR / "vms" / self.vm_name
        self.vmx_path = self.vm_dir / f"{self.vm_name}.vmx"

    def check_vmware_fusion(self) -> bool:
        """Check if VMware Fusion Pro is installed."""
        if not VMWARE_APP.exists():
            console.error(
                "VMware Fusion Pro is not installed. "
                "Download from: https://www.vmware.com/products/fusion.html"
            )
            return False

        if not VMRUN.exists():
            console.error(f"vmrun not found at {VMRUN}")
            return False

        console.success("VMware Fusion Pro found")
        return True

    def check_qemu_img(self) -> bool:
        """Check if qemu-img is available (needed for ARM64)."""
        return shutil.which("qemu-img") is not None

    def _vmdk_metadata_path(self, vmdk_path: Path) -> Path:
        return vmdk_path.with_name(f"{vmdk_path.name}.meta.json")

    def _vmdk_is_fresh(
        self, vmdk_path: Path, image_path: Path, image_format: str
    ) -> bool:
        meta_path = self._vmdk_metadata_path(vmdk_path)
        if not vmdk_path.exists() or not meta_path.exists():
            return False
        try:
            data = json.loads(meta_path.read_text())
        except (OSError, json.JSONDecodeError):
            return False

        stat = image_path.stat()
        return (
            data.get("source_name") == image_path.name
            and data.get("source_size") == stat.st_size
            and data.get("source_mtime_ns") == stat.st_mtime_ns
            and data.get("source_format") == image_format
        )

    def _write_vmdk_metadata(
        self, vmdk_path: Path, image_path: Path, image_format: str
    ) -> None:
        stat = image_path.stat()
        data = {
            "source_name": image_path.name,
            "source_size": stat.st_size,
            "source_mtime_ns": stat.st_mtime_ns,
            "source_format": image_format,
        }
        meta_path = self._vmdk_metadata_path(vmdk_path)
        meta_path.write_text(json.dumps(data, indent=2))

    def convert_qcow2_to_vmdk(self, qcow2_path: Path, vmdk_path: Path) -> bool:
        """Convert QCOW2 image to VMDK format using qemu-img."""
        console.info("Converting QCOW2 to VMDK (this may take a moment)...")

        result = subprocess.run(
            [
                "qemu-img",
                "convert",
                "-f",
                "qcow2",
                "-O",
                "vmdk",
                "-o",
                "adapter_type=lsilogic",  # Compatible with VMware
                str(qcow2_path),
                str(vmdk_path),
            ],
            capture_output=True,
            text=True,
        )

        if result.returncode == 0:
            console.success("Disk converted successfully")
            return True

        console.error(f"Conversion failed: {result.stderr}")
        return False

    def prepare_disk(self, image_path: Path, image_format: str) -> Path:
        """Prepare VMDK from downloaded image.

        Handles both OVA (x86_64) and QCOW2 (ARM64) formats.
        """
        self.vm_dir.mkdir(parents=True, exist_ok=True)

        vmdk_path = self.vm_dir / f"{image_path.stem}.vmdk"

        if self._vmdk_is_fresh(vmdk_path, image_path, image_format):
            console.info("Using cached VM disk")
            return vmdk_path
        if vmdk_path.exists():
            console.info("Cached VM disk is stale; rebuilding...")
            vmdk_path.unlink()
            self._vmdk_metadata_path(vmdk_path).unlink(missing_ok=True)

        if image_format == "qcow2":
            # ARM64: Convert QCOW2 to VMDK
            if not self.check_qemu_img():
                console.error(
                    "qemu-img is required for ARM64 Macs.\n"
                    "Install with: brew install qemu"
                )
                raise RuntimeError("qemu-img not found")

            if not self.convert_qcow2_to_vmdk(image_path, vmdk_path):
                raise RuntimeError("Failed to convert QCOW2 to VMDK")

            self._write_vmdk_metadata(vmdk_path, image_path, image_format)
            return vmdk_path

        # OVA format (x86_64)
        console.info("Importing Ubuntu cloud OVA...")

        # Use ovftool to extract VMDK from OVA
        if OVFTOOL.exists():
            result = subprocess.run(
                [
                    str(OVFTOOL),
                    "--lax",  # Be lenient with OVA format
                    "--diskMode=monolithicSparse",
                    str(image_path),
                    str(self.vm_dir / "imported.vmx"),
                ],
                capture_output=True,
                text=True,
            )
            if result.returncode == 0:
                # Find the extracted VMDK
                vmdk_files = sorted(
                    self.vm_dir.glob("*.vmdk"),
                    key=lambda path: path.stat().st_mtime,
                    reverse=True,
                )
                if vmdk_files:
                    vmdk = vmdk_files[0]
                    if vmdk.name != vmdk_path.name:
                        vmdk.rename(vmdk_path)
                # Clean up the imported VMX
                for f in self.vm_dir.glob("imported.*"):
                    if f.suffix != ".vmdk":
                        f.unlink()
                if vmdk_path.exists():
                    self._write_vmdk_metadata(vmdk_path, image_path, image_format)
                    console.success("OVA imported successfully")
                    return vmdk_path

        # Fallback: Extract OVA manually (it's a tar file)
        console.info("Extracting OVA manually...")
        with tarfile.open(image_path, "r") as tar:
            for member in tar.getmembers():
                if member.name.endswith(".vmdk"):
                    member.name = vmdk_path.name
                    tar.extract(member, self.vm_dir, filter="data")
                    self._write_vmdk_metadata(vmdk_path, image_path, image_format)
                    console.success("VMDK extracted from OVA")
                    return vmdk_path

        raise RuntimeError("Could not extract VMDK from OVA")

    def create_cloud_init_iso(self) -> Path:
        """Create cloud-init ISO with Ventoy installation script."""
        iso_path = self.vm_dir / "cloud-init.iso"
        staging_dir = self.vm_dir / "cloud-init-staging"

        # Clean up any existing staging
        if staging_dir.exists():
            shutil.rmtree(staging_dir)
        staging_dir.mkdir(parents=True)

        # Create meta-data
        meta_data = staging_dir / "meta-data"
        meta_data.write_text("instance-id: ventoy-installer\nlocal-hostname: ventoy\n")

        # Create user-data with Ventoy installation script
        user_data = staging_dir / "user-data"
        user_data.write_text(f"""#cloud-config
users:
  - name: ubuntu
    sudo: ALL=(ALL) NOPASSWD:ALL
    shell: /bin/bash
    lock_passwd: false

# Set password for vmrun authentication
chpasswd:
  expire: false
  users:
    - name: ubuntu
      password: ubuntu
      type: text

write_files:
  - path: /opt/install-ventoy.sh
    permissions: '0755'
    content: |
      #!/bin/bash
      set -ex

      echo "=== Ventoy USB Installation Script ==="

      # Download Ventoy
      echo "Downloading Ventoy {VENTOY_VERSION}..."
      cd /opt
      wget -q https://github.com/ventoy/Ventoy/releases/download/v{VENTOY_VERSION}/ventoy-{VENTOY_VERSION}-linux.tar.gz
      tar xzf ventoy-{VENTOY_VERSION}-linux.tar.gz
      rm ventoy-{VENTOY_VERSION}-linux.tar.gz

      # Wait for USB device to appear
      echo "Waiting for USB device..."
      for i in {{1..30}}; do
        for dev in /dev/sd?; do
          if [ -b "$dev" ]; then
            devname=$(basename "$dev")
            if [ -f "/sys/block/${{devname}}/removable" ]; then
              removable=$(cat "/sys/block/${{devname}}/removable")
              if [ "$removable" = "1" ]; then
                size=$(cat "/sys/block/${{devname}}/size")
                if [ "$size" -gt 1000000 ]; then
                  USB_DEV="$dev"
                  echo "Found USB device: $USB_DEV"
                  break 2
                fi
              fi
            fi
          fi
        done
        sleep 1
      done

      if [ -z "$USB_DEV" ]; then
        echo "ERROR: No USB device found!"
        echo "VENTOY_INSTALL_FAILED" > /tmp/ventoy-status
        exit 1
      fi

      # Install Ventoy
      echo "Installing Ventoy on $USB_DEV..."
      cd /opt/ventoy-{VENTOY_VERSION}

      # Run Ventoy installation (non-interactive, force install)
      echo "y" | ./Ventoy2Disk.sh -I "$USB_DEV"

      if [ $? -eq 0 ]; then
        echo "VENTOY_INSTALL_SUCCESS" > /tmp/ventoy-status
        echo "=== Ventoy installation complete ==="
        # Signal success by shutting down
        sleep 2
        poweroff
      else
        echo "VENTOY_INSTALL_FAILED" > /tmp/ventoy-status
        echo "=== Ventoy installation FAILED ==="
        exit 1
      fi

runcmd:
  - /opt/install-ventoy.sh

final_message: "Cloud-init completed. Ventoy installation status in /tmp/ventoy-status"
""")

        # Create ISO using hdiutil (macOS)
        console.info("Creating cloud-init ISO...")
        result = subprocess.run(
            [
                "hdiutil",
                "makehybrid",
                "-iso",
                "-joliet",
                "-iso-volume-name",
                "cidata",
                "-joliet-volume-name",
                "cidata",
                "-o",
                str(iso_path),
                str(staging_dir),
            ],
            capture_output=True,
            text=True,
        )
        if result.returncode != 0:
            console.error(f"Failed to create ISO: {result.stderr}")
            raise RuntimeError("Failed to create cloud-init ISO")

        # Cleanup staging
        shutil.rmtree(staging_dir)

        console.success("Cloud-init ISO created")
        return iso_path

    def generate_vmx(self, vmdk_path: Path, cloud_init_iso: Path, vid_pid: str) -> Path:
        """Generate VMX configuration file for Ubuntu cloud image."""
        self.vm_dir.mkdir(parents=True, exist_ok=True)

        vid, pid = vid_pid.split(":")

        arch = get_host_arch()
        guest_os = "arm-ubuntu-64" if arch == "arm64" else "ubuntu-64"

        template_name = (
            "vmx-arm64.template" if arch == "arm64" else "vmx-x86_64.template"
        )
        template_path = SCRIPT_DIR / "templates" / template_name

        if not template_path.exists():
            raise FileNotFoundError(f"VMX template not found: {template_path}")

        template = template_path.read_text()

        vmx_content = template.format(
            vm_name=self.vm_name,
            guest_os=guest_os,
            vmdk_path=vmdk_path,
            cloud_init_iso=cloud_init_iso,
            vid=vid,
            pid=pid,
        )

        self.vmx_path.write_text(vmx_content)
        console.success("VMX configuration generated")
        return self.vmx_path

    def start_vm(self) -> bool:
        """Start the VM."""
        console.info("Starting VM...")
        result = subprocess.run(
            [str(VMRUN), "-T", "fusion", "start", str(self.vmx_path), "gui"],
            capture_output=True,
            text=True,
        )
        if result.returncode == 0:
            console.success("VM started")
            return True
        console.error(f"Failed to start VM: {result.stderr}")
        return False

    def is_vm_running(self) -> bool:
        """Check if the VM is currently running."""
        result = subprocess.run(
            [str(VMRUN), "list"],
            capture_output=True,
            text=True,
        )
        return str(self.vmx_path) in result.stdout

    def wait_for_ventoy_install(self, timeout: int = 300) -> bool:
        """Wait for Ventoy installation to complete (VM will shut down on success)."""
        console.info(
            "Waiting for Ventoy installation (VM will shut down when complete)..."
        )
        start_time = time.time()

        while time.time() - start_time < timeout:
            # VM shuts down on successful installation
            if not self.is_vm_running():
                console.success("VM shut down - Ventoy installation complete!")
                return True

            elapsed = int(time.time() - start_time)
            if elapsed % 15 == 0 and elapsed > 0:
                console.info(f"Still installing Ventoy... ({elapsed}s)")

            time.sleep(5)

        console.error("Timeout waiting for Ventoy installation")
        return False

    def stop_vm(self) -> None:
        """Stop the VM if running."""
        result = subprocess.run(
            [str(VMRUN), "list"],
            capture_output=True,
            text=True,
        )
        if str(self.vmx_path) in result.stdout:
            console.info("Stopping VM...")
            subprocess.run(
                [str(VMRUN), "stop", str(self.vmx_path), "soft"],
                capture_output=True,
            )
            time.sleep(2)

    def cleanup(self) -> None:
        """Clean up VM files while preserving cached disks."""
        self.stop_vm()
        if not self.vm_dir.exists():
            return

        for path in self.vm_dir.iterdir():
            if path.is_dir():
                shutil.rmtree(path)
                continue
            if path.suffix == ".vmdk" or path.name.endswith(".vmdk.meta.json"):
                continue
            path.unlink()

        console.success("VM artifacts cleaned up")


# =============================================================================
# CLI
# =============================================================================


def check_prerequisites(skip_ventoy: bool) -> bool:
    """Check all prerequisites are met."""
    console.info("Checking prerequisites...")

    # Check for macOS
    import platform

    if platform.system() != "Darwin":
        console.error("This script only runs on macOS")
        return False

    # Check for VMware Fusion (only if installing Ventoy)
    if not skip_ventoy:
        vmware = VMwareManager()
        if not vmware.check_vmware_fusion():
            return False

        # ARM64 Macs need qemu-img to convert Ubuntu cloud images
        if get_host_arch() == "arm64" and not vmware.check_qemu_img():
            console.error(
                "qemu-img is required for ARM64 Macs (to convert Ubuntu cloud images).\n"
                "Install with: brew install qemu"
            )
            return False
        if get_host_arch() == "arm64":
            console.success("qemu-img found")

    # Check for VyOS config file
    if not VYOS_CONFIG.exists():
        console.error(f"VyOS configuration file not found: {VYOS_CONFIG}")
        return False
    console.success("VyOS configuration file found")

    return True


def select_device(usb_mgr: USBDeviceManager, device: str | None) -> USBDevice | None:
    """Select a USB device interactively or by name."""
    if device:
        info = usb_mgr.get_device_info(device)
        if not info:
            console.error(f"Device not found: {device}")
            return None
        return info

    # List devices
    console.info("Detecting USB devices...")
    devices = usb_mgr.list_external_devices()

    if not devices:
        console.error("No external USB devices detected")
        return None

    # Show table
    console.table(
        "Available USB Devices",
        ["Identifier", "Name", "Size"],
        [[d.identifier, d.name, d.size] for d in devices],
    )

    # Prompt for selection
    identifier = console.prompt("Enter the disk identifier to use (e.g., disk4)")
    return usb_mgr.get_device_info(identifier)


def confirm_device(device: USBDevice, skip_ventoy: bool, yes: bool) -> bool:
    """Confirm device selection."""
    console.console.print()
    if not skip_ventoy:
        console.warn("WARNING: This will ERASE ALL DATA on the following device:")
    else:
        console.info("Target USB device:")

    console.console.print(f"\n  Device: /dev/{device.identifier}")
    console.console.print(f"  Name:   {device.name}")
    console.console.print(f"  Size:   {device.size}\n")

    if yes:
        console.info("Skipping confirmation (--yes flag)")
        return True

    if not skip_ventoy:
        response = console.prompt("Type 'yes' to continue, or anything else to abort")
        return response.lower() == "yes"

    return console.confirm("Continue?", default=True)


def copy_files_to_usb(
    mount_point: Path, vyos_iso: Path | None, talos_iso: Path | None
) -> None:
    """Copy ISOs and config to USB."""
    console.info("Copying files to USB...")

    if vyos_iso and vyos_iso.exists():
        console.info(
            f"Copying VyOS ISO ({vyos_iso.stat().st_size // 1024 // 1024}MB)..."
        )
        shutil.copy2(vyos_iso, mount_point / vyos_iso.name)
        console.success("VyOS ISO copied")

    if talos_iso and talos_iso.exists():
        console.info(
            f"Copying Talos ISO ({talos_iso.stat().st_size // 1024 // 1024}MB)..."
        )
        shutil.copy2(talos_iso, mount_point / talos_iso.name)
        console.success("Talos ISO copied")

    if VYOS_CONFIG.exists():
        console.info("Copying VyOS configuration...")
        shutil.copy2(VYOS_CONFIG, mount_point / VYOS_CONFIG.name)
        console.success("VyOS configuration copied")

    console.success("All files copied to USB")


@click.command()
@click.option("-d", "--device", help="USB device to provision (e.g., disk4)")
@click.option(
    "-s", "--skip-download", is_flag=True, help="Skip ISO download, use cached files"
)
@click.option("-v", "--skip-ventoy", is_flag=True, help="Skip Ventoy installation")
@click.option("-y", "--yes", is_flag=True, help="Skip confirmation prompts")
def main(device: str | None, skip_download: bool, skip_ventoy: bool, yes: bool) -> None:
    """Provision a USB drive with Ventoy and lab bootstrap media."""
    console.banner("Lab Bootstrap USB Provisioning")

    # Check prerequisites
    if not check_prerequisites(skip_ventoy):
        raise SystemExit(1)

    # Load e2 credentials for S3 downloads
    e2_credentials = None
    if not skip_download:
        try:
            console.info("Loading e2 storage credentials...")
            e2_credentials = load_e2_credentials()
            console.success("e2 credentials loaded")
        except FileNotFoundError as e:
            console.error(str(e))
            raise SystemExit(1)
        except RuntimeError as e:
            console.error(str(e))
            raise SystemExit(1)

    # Initialize managers
    usb_mgr = USBDeviceManager()
    download_mgr = DownloadManager(credentials=e2_credentials)

    # Select and confirm device
    usb_device = select_device(usb_mgr, device)
    if not usb_device:
        raise SystemExit(1)

    if not confirm_device(usb_device, skip_ventoy, yes):
        console.error("Aborted by user")
        raise SystemExit(1)

    # Download ISOs
    vyos_iso = None
    talos_iso = None
    if not skip_download:
        vyos_iso = download_mgr.download_vyos_iso()
        if not vyos_iso:
            console.error("Failed to download VyOS ISO - cannot continue")
            raise SystemExit(1)

        talos_iso = download_mgr.download_talos_iso()
        if not talos_iso:
            console.error("Failed to download Talos ISO - cannot continue")
            raise SystemExit(1)
    else:
        # Try to resolve paths for cache checking even if we skipped download
        vyos_path = download_mgr.vyos_path
        talos_path = download_mgr.talos_path

        # If config loading failed earlier, try to load again or fail
        if not vyos_path or not talos_path:
            try:
                vyos_path, talos_path = load_iso_config()
            except Exception as e:
                console.error(f"Cannot resolve ISO paths from manifest: {e}")
                raise SystemExit(1)

        console.info("Skipping ISO download (--skip-download flag)")
        vyos_iso = ISO_CACHE_DIR / Path(vyos_path).name
        talos_iso = ISO_CACHE_DIR / Path(talos_path).name
        if not vyos_iso.exists():
            console.error(f"VyOS ISO not found in cache: {vyos_iso}")
            raise SystemExit(1)
        if not talos_iso.exists():
            console.error(f"Talos ISO not found in cache: {talos_iso}")
            raise SystemExit(1)

    # Install Ventoy
    if not skip_ventoy:
        vmware = VMwareManager()

        # Get VID:PID
        vid_pid = usb_mgr.extract_vid_pid(usb_device.identifier)
        if not vid_pid:
            console.warn("Could not detect USB VID:PID automatically")
            vid_pid = console.prompt("Enter USB VID:PID manually (format: xxxx:xxxx)")
            if not re.match(r"^[0-9a-fA-F]{4}:[0-9a-fA-F]{4}$", vid_pid):
                console.error("Invalid VID:PID format")
                raise SystemExit(1)

        console.info(f"USB device VID:PID: {vid_pid}")

        # Unmount USB before VM operations
        console.info("Unmounting USB device...")
        usb_mgr.unmount(usb_device.identifier)

        try:
            # Download VM requirements
            ubuntu_result = download_mgr.download_ubuntu_image()

            if not ubuntu_result:
                console.error("Failed to download Ubuntu cloud image")
                raise SystemExit(1)

            ubuntu_image, image_format = ubuntu_result

            # Prepare disk (handles OVA for x86_64, QCOW2 conversion for ARM64)
            vmdk_path = vmware.prepare_disk(ubuntu_image, image_format)
            cloud_init_iso = vmware.create_cloud_init_iso()
            vmware.generate_vmx(vmdk_path, cloud_init_iso, vid_pid)

            # Start VM
            if not vmware.start_vm():
                raise SystemExit(1)

            # Wait for Ventoy installation (VM shuts down on success)
            if not vmware.wait_for_ventoy_install(timeout=300):
                console.error("Ventoy installation did not complete successfully")
                raise SystemExit(1)

        finally:
            vmware.cleanup()

        console.success("Ventoy installation complete")
    else:
        console.info("Skipping Ventoy installation (--skip-ventoy flag)")

    # Wait for Ventoy partition
    console.info("Waiting for Ventoy partition...")
    time.sleep(3)  # Give macOS time to detect the new partition
    if not usb_mgr.wait_for_partition(usb_device.identifier):
        console.error("Ventoy partition not found")
        raise SystemExit(1)
    console.success("Ventoy partition detected")

    # Mount and copy files
    mount_point = usb_mgr.mount_partition(usb_device.identifier)
    if not mount_point:
        console.error("Failed to mount Ventoy partition")
        raise SystemExit(1)
    console.success(f"Ventoy partition mounted at: {mount_point}")

    copy_files_to_usb(mount_point, vyos_iso, talos_iso)

    # Eject USB
    console.info("Ejecting USB device...")
    usb_mgr.eject(usb_device.identifier)
    console.success("USB device ejected")

    # Done!
    console.console.print()
    console.banner("USB Provisioning Complete!")
    console.console.print()
    console.console.print("The USB drive is now ready for lab bootstrap.")
    console.console.print()
    console.console.print("[bold]Contents:[/bold]")
    console.console.print("  - Ventoy bootloader installed")
    console.console.print("  - VyOS Stream ISO (for router installation)")
    console.console.print("  - Talos ISO with embedded config (for UM760 bootstrap)")
    console.console.print("  - gateway.conf (VyOS configuration)")
    console.console.print()
    console.console.print("[bold]Next steps:[/bold]")
    console.console.print(
        "  1. Boot VP6630 from USB -> Select VyOS ISO -> Run 'install image'"
    )
    console.console.print("  2. After VyOS install, load gateway.conf configuration")
    console.console.print(
        "  3. Boot UM760 from USB -> Select Talos ISO -> Bootstrap completes"
    )
    console.console.print()
    console.console.print(
        "See docs/architecture/appendices/B_bootstrap_procedure.md for details."
    )
    console.console.print()


if __name__ == "__main__":
    main()
