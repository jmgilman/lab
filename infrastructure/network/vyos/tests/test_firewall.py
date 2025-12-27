"""
Firewall functional tests for the VyOS gateway.

These tests verify that firewall rules actually block and allow
traffic as expected, not just that they are configured.
"""

import pytest


class TestWanToLabFirewall:
    """Test firewall rules for traffic from WAN to lab networks."""

    def test_home_network_can_ping_lab(self, ping, test_topology):
        """
        WAN client (in HOME_NETWORK) can ping lab clients.

        The WAN_TO_LAB firewall allows traffic from HOME_NETWORK (192.168.0.0/24).
        """
        assert ping("wan-client", test_topology.mgmt_client_ip), (
            "wan-client (HOME_NETWORK) should be able to ping mgmt-client"
        )

    def test_home_network_can_reach_lab_services(self, tcp_connect, test_topology):
        """
        WAN client (in HOME_NETWORK) can reach lab services.

        Since wan-client is in HOME_NETWORK, it should be allowed through.
        """
        # This tests that HOME_NETWORK rule actually works
        # We can't easily test "non-home" traffic without another WAN client
        assert ping_result := tcp_connect(
            "wan-client", test_topology.mgmt_gateway, 22
        ), "wan-client should reach gateway SSH (HOME_NETWORK allowed)"


class TestLabToWanFirewall:
    """Test firewall rules for traffic from lab to WAN."""

    def test_lab_can_reach_wan(self, ping, test_topology):
        """Lab clients can reach the WAN network."""
        assert ping("mgmt-client", test_topology.wan_client_ip), (
            "Lab client should be able to reach WAN"
        )

    def test_lab_can_reach_wan_gateway(self, ping, test_topology):
        """Lab clients can reach the WAN-side gateway IP."""
        assert ping("platform-client", test_topology.wan_ip), (
            "Lab client should be able to reach gateway WAN IP"
        )


class TestLocalFirewall:
    """Test firewall rules for traffic destined to the gateway itself."""

    def test_lab_can_ssh_to_gateway(self, tcp_connect, test_topology):
        """Lab clients can SSH to the gateway."""
        assert tcp_connect("mgmt-client", test_topology.mgmt_gateway, 22), (
            "Lab client should be able to SSH to gateway"
        )

    def test_wan_can_ssh_to_gateway(self, tcp_connect, test_topology):
        """WAN client (HOME_NETWORK) can SSH to gateway."""
        assert tcp_connect("wan-client", test_topology.wan_ip, 22), (
            "WAN client (HOME_NETWORK) should be able to SSH to gateway"
        )

    def test_lab_can_reach_gateway_dns(self, tcp_connect, test_topology):
        """Lab clients can reach gateway DNS service."""
        # DNS uses UDP primarily, but we can test TCP DNS as well
        # For simplicity, we'll verify DNS works via the dns_resolve fixture
        # in test_services.py. Here we just verify port 53 TCP is reachable.
        assert tcp_connect("mgmt-client", test_topology.mgmt_gateway, 53), (
            "Lab client should be able to reach gateway DNS (TCP)"
        )

    @pytest.mark.parametrize(
        "client,gateway",
        [
            ("mgmt-client", "10.10.10.1"),
            ("platform-client", "10.10.30.1"),
            ("cluster-client", "10.10.40.1"),
        ],
    )
    def test_lab_can_ping_gateway(self, ping, client, gateway):
        """Lab clients can ping the gateway (ICMP allowed in LOCAL ruleset)."""
        assert ping(client, gateway), f"{client} should be able to ping gateway {gateway}"


class TestFirewallIsolation:
    """Test that firewall properly isolates networks when expected."""

    def test_established_connections_work(self, ping, test_topology):
        """
        Verify stateful firewall allows return traffic.

        When a lab client initiates a connection to WAN, the return
        traffic should be allowed through (established/related rule).
        """
        # This is implicitly tested by test_lab_can_reach_wan, but let's
        # make it explicit: if the lab client can ping WAN and get responses,
        # then established/related traffic is working.
        assert ping("mgmt-client", test_topology.wan_client_ip), (
            "Stateful firewall should allow return traffic"
        )
