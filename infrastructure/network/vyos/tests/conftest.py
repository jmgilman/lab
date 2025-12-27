"""
Pytest fixtures for VyOS Gateway functional tests.

This module provides fixtures for testing the VyOS gateway running in Containerlab.
Tests validate actual network behavior, not just configuration strings.
"""

import os
import socket
import subprocess
import time
from dataclasses import dataclass
from typing import Callable

import pytest
from scrapli import Scrapli


def wait_for_vyos_ready(host: str, timeout: int = 240, interval: int = 5) -> bool:
    """Wait for VyOS to be ready for SSH connections."""
    start_time = time.time()
    while time.time() - start_time < timeout:
        try:
            sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
            sock.settimeout(5)
            result = sock.connect_ex((host, 22))
            sock.close()
            if result == 0:
                # SSH port is open, wait a bit more for VyOS to fully initialize
                time.sleep(10)
                return True
        except socket.error:
            pass
        time.sleep(interval)
    return False


def normalize_output(output: str) -> str:
    """Normalize VyOS CLI output for stable assertions."""
    warning_lines = {
        "WARNING: terminal is not fully functional",
        "Press RETURN to continue",
    }
    filtered = [line for line in output.splitlines() if line.strip() not in warning_lines]
    return "\n".join(filtered).replace("'", "")


@dataclass(frozen=True)
class TestTopology:
    """Expected values for the Containerlab test topology."""

    # WAN interface
    wan_iface: str = "eth4"
    wan_ip: str = "192.168.0.2"
    wan_cidr: str = "192.168.0.2/24"
    wan_gateway: str = "192.168.0.1"
    wan_client_ip: str = "192.168.0.100"

    # Trunk interface
    trunk_iface: str = "eth5"

    # VLAN networks (gateway IPs)
    mgmt_vif: str = "10"
    mgmt_gateway: str = "10.10.10.1"
    mgmt_client_ip: str = "10.10.10.100"

    prov_vif: str = "20"
    prov_gateway: str = "10.10.20.1"
    prov_client_ip: str = "10.10.20.100"

    platform_vif: str = "30"
    platform_gateway: str = "10.10.30.1"
    platform_client_ip: str = "10.10.30.100"

    cluster_vif: str = "40"
    cluster_gateway: str = "10.10.40.1"
    cluster_client_ip: str = "10.10.40.100"

    service_vif: str = "50"
    service_gateway: str = "10.10.50.1"
    service_client_ip: str = "10.10.50.100"

    storage_vif: str = "60"
    storage_gateway: str = "10.10.60.1"
    storage_client_ip: str = "10.10.60.100"

    # Network ranges
    home_cidr: str = "192.168.0.0/24"
    lab_cidr: str = "10.10.0.0/16"

    # DHCP configuration
    dhcp_subnet: str = "10.10.10.0/24"
    dhcp_range_start: str = "10.10.10.200"
    dhcp_range_stop: str = "10.10.10.250"

    # DNS configuration
    dns_listen_addresses: tuple[str, ...] = ("10.10.10.1", "10.10.30.1")

    # BGP configuration
    bgp_neighbors: tuple[str, ...] = ("10.10.30.10", "10.10.30.11", "10.10.30.12")
    bgp_remote_as: str = "64513"
    bgp_local_as: str = "64512"
    bgp_router_id: str = "10.10.50.1"
    bgp_service_network: str = "10.10.50.0/24"

    # System configuration
    domain_name: str = "lab.gilman.io"
    hostname: str = "gateway"
    name_servers: tuple[str, ...] = ("1.1.1.1", "8.8.8.8")

    # Container name prefix
    container_prefix: str = "clab-vyos-gateway-test"


# Mapping of client names to their gateway IPs for connectivity tests
VLAN_CLIENTS = {
    "mgmt-client": "10.10.10.1",
    "prov-client": "10.10.20.1",
    "platform-client": "10.10.30.1",
    "cluster-client": "10.10.40.1",
    "service-client": "10.10.50.1",
    "storage-client": "10.10.60.1",
}


@pytest.fixture(scope="session")
def vyos_host() -> str:
    """Get the VyOS gateway hostname from environment or use default."""
    return os.environ.get("VYOS_HOST", "clab-vyos-gateway-test-gateway")


@pytest.fixture(scope="session")
def vyos_container(vyos_host: str) -> str:
    """Get the VyOS container name for docker exec."""
    return os.environ.get("VYOS_CONTAINER", vyos_host)


@pytest.fixture(scope="session")
def vyos_username() -> str:
    """Get the VyOS username from environment or use default."""
    return os.environ.get("VYOS_USER", "vyos")


@pytest.fixture(scope="session")
def vyos_password() -> str:
    """Get the VyOS password from environment or use default."""
    return os.environ.get("VYOS_PASS", "vyos")


@pytest.fixture(scope="session")
def vyos_private_key() -> str | None:
    """Get the path to the VyOS SSH private key, if provided."""
    env_key = os.environ.get("VYOS_SSH_KEY")
    if env_key:
        return env_key
    default_key = os.path.join(os.path.dirname(__file__), ".vyos-test-key")
    return default_key if os.path.exists(default_key) else None


@pytest.fixture(scope="session")
def test_topology() -> TestTopology:
    """Return the expected topology values for assertions."""
    return TestTopology()


@pytest.fixture(scope="session")
def vyos(
    vyos_host: str,
    vyos_username: str,
    vyos_password: str,
    vyos_private_key: str | None,
) -> Scrapli:
    """
    Create a Scrapli connection to the VyOS gateway.

    This fixture uses session scope so the connection is reused across all tests.
    """
    if not wait_for_vyos_ready(vyos_host):
        pytest.fail(f"VyOS at {vyos_host} not ready after timeout")

    conn_args = {
        "host": vyos_host,
        "auth_username": vyos_username,
        "auth_strict_key": False,
        "transport": "system",
        "platform": "vyos_vyos",
        "transport_options": {"open_cmd": ["-tt"]},
    }
    if vyos_private_key:
        conn_args["auth_private_key"] = vyos_private_key
    else:
        conn_args["auth_password"] = vyos_password

    conn = Scrapli(**conn_args)
    conn.open()
    yield conn
    conn.close()


@pytest.fixture(scope="session")
def vyos_show(vyos: Scrapli) -> Callable[[str], str]:
    """Return a helper to run VyOS show commands and return normalized output."""

    def _show(command: str) -> str:
        result = vyos.send_command(command)
        if result.failed:
            pytest.fail(f"Command failed: {command}")
        return normalize_output(result.result)

    return _show


@pytest.fixture(scope="session")
def exec_on_client(test_topology: TestTopology) -> Callable[..., subprocess.CompletedProcess]:
    """
    Execute a command on a test client container.

    Usage:
        result = exec_on_client("mgmt-client", ["ping", "-c", "1", "10.10.10.1"])
        assert result.returncode == 0
    """

    def _exec(
        client: str,
        cmd: list[str],
        timeout: int = 30,
    ) -> subprocess.CompletedProcess:
        container = f"{test_topology.container_prefix}-{client}"
        return subprocess.run(
            ["docker", "exec", container, *cmd],
            capture_output=True,
            text=True,
            timeout=timeout,
        )

    return _exec


@pytest.fixture(scope="session")
def ping(exec_on_client: Callable) -> Callable[[str, str, int], bool]:
    """
    Ping helper that returns True if ping succeeds.

    Usage:
        assert ping("mgmt-client", "10.10.10.1")
    """

    def _ping(from_client: str, target: str, count: int = 3) -> bool:
        result = exec_on_client(from_client, ["ping", "-c", str(count), "-W", "2", target])
        return result.returncode == 0

    return _ping


@pytest.fixture(scope="session")
def tcp_connect(exec_on_client: Callable) -> Callable[[str, str, int, int], bool]:
    """
    Test TCP connectivity using netcat.

    Returns True if connection succeeds, False otherwise.

    Usage:
        assert tcp_connect("mgmt-client", "10.10.10.1", 22)  # SSH should work
        assert not tcp_connect("wan-client", "10.10.10.100", 22)  # Should be blocked
    """

    def _connect(from_client: str, target: str, port: int, timeout: int = 3) -> bool:
        result = exec_on_client(
            from_client,
            ["nc", "-z", "-w", str(timeout), target, str(port)],
        )
        return result.returncode == 0

    return _connect


@pytest.fixture(scope="session")
def dns_resolve(exec_on_client: Callable) -> Callable[[str, str, str], str | None]:
    """
    Resolve a DNS name using dig.

    Returns the resolved IP or None if resolution fails.

    Usage:
        ip = dns_resolve("mgmt-client", "cloudflare.com", "10.10.10.1")
        assert ip is not None
    """

    def _resolve(from_client: str, hostname: str, dns_server: str) -> str | None:
        result = exec_on_client(
            from_client,
            ["dig", "+short", f"@{dns_server}", hostname],
        )
        if result.returncode != 0:
            return None
        # dig returns IPs one per line, take the first valid one
        for line in result.stdout.strip().split("\n"):
            line = line.strip()
            if line and not line.startswith(";"):
                return line
        return None

    return _resolve


@pytest.fixture(scope="session")
def vlan_clients() -> dict[str, str]:
    """Return mapping of VLAN client names to their gateway IPs."""
    return VLAN_CLIENTS.copy()
