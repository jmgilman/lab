"""
NAT functional tests for the VyOS gateway.

These tests verify that NAT masquerading actually translates
source addresses correctly.
"""

import subprocess
import time


class TestSourceNat:
    """Test source NAT (masquerade) functionality."""

    def test_nat_masquerade_translates_source(
        self, exec_on_client, test_topology
    ):
        """
        Traffic from lab networks appears with gateway's WAN IP on WAN side.

        This test captures traffic on wan-client while mgmt-client pings it,
        verifying the source IP is the gateway's WAN IP (masqueraded).
        """
        wan_client = f"{test_topology.container_prefix}-wan-client"
        mgmt_client = f"{test_topology.container_prefix}-mgmt-client"

        # Start tcpdump on wan-client in background, capturing ICMP
        tcpdump_proc = subprocess.Popen(
            [
                "docker", "exec", wan_client,
                "tcpdump", "-i", "eth1", "-c", "3", "-n", "icmp",
            ],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )

        # Give tcpdump a moment to start
        time.sleep(1)

        # Send pings from mgmt-client to wan-client
        subprocess.run(
            ["docker", "exec", mgmt_client, "ping", "-c", "3", "-W", "2",
             test_topology.wan_client_ip],
            capture_output=True,
            timeout=10,
        )

        # Wait for tcpdump to finish and get output
        try:
            stdout, stderr = tcpdump_proc.communicate(timeout=10)
        except subprocess.TimeoutExpired:
            tcpdump_proc.kill()
            stdout, stderr = tcpdump_proc.communicate()

        # Verify the captured packets show gateway WAN IP as source
        # The original source (10.10.10.100) should NOT appear
        assert test_topology.wan_ip in stdout, (
            f"Expected NAT'd source IP {test_topology.wan_ip} in capture, "
            f"got: {stdout}"
        )
        assert test_topology.mgmt_client_ip not in stdout, (
            f"Original source IP {test_topology.mgmt_client_ip} should not "
            f"appear in WAN-side capture (NAT should hide it)"
        )

    def test_nat_allows_bidirectional_traffic(self, ping, test_topology):
        """
        NAT connection tracking allows return traffic.

        When a lab client initiates to WAN, responses come back correctly.
        """
        # If ping succeeds, it means:
        # 1. Outbound packet was NAT'd (source changed to gateway WAN IP)
        # 2. Return packet was correctly de-NAT'd back to original source
        assert ping("mgmt-client", test_topology.wan_client_ip), (
            "NAT connection tracking should allow bidirectional traffic"
        )

    def test_multiple_vlans_share_nat(self, ping, test_topology):
        """All VLAN clients can use NAT to reach WAN."""
        clients = [
            "mgmt-client",
            "prov-client",
            "platform-client",
            "cluster-client",
            "service-client",
            "storage-client",
        ]
        for client in clients:
            assert ping(client, test_topology.wan_client_ip), (
                f"{client} should be able to reach WAN via NAT"
            )
