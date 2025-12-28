"""
Service functional tests for the VyOS gateway.

These tests verify that gateway services (DNS, SSH) actually work,
not just that they are configured.

Note: DHCP tests are not included because DHCP broadcast delivery through
VLAN subinterfaces doesn't work in containerlab's veth/bridge setup. This is
a fundamental Linux networking limitation - the bridge's frame processing
intercepts packets before the 8021q module can deliver them to subinterfaces.
"""

import pytest


class TestDnsService:
    """Test DNS forwarding service."""

    def test_dns_resolves_external_domain(self, dns_resolve, test_topology):
        """
        DNS forwarding resolves external domains.

        The gateway should forward DNS queries to upstream resolvers
        and return valid responses.
        """
        # Use a well-known domain that should always resolve
        result = dns_resolve("mgmt-client", "one.one.one.one", test_topology.mgmt_gateway)
        assert result is not None, (
            "DNS resolution via gateway failed for one.one.one.one"
        )
        # Cloudflare's one.one.one.one resolves to 1.1.1.1 or 1.0.0.1
        assert result.startswith("1."), (
            f"Unexpected DNS result for one.one.one.one: {result}"
        )

    @pytest.mark.parametrize(
        "client,dns_server",
        [
            ("mgmt-client", "10.10.10.1"),
            ("platform-client", "10.10.30.1"),
        ],
    )
    def test_dns_available_on_multiple_interfaces(
        self, dns_resolve, client, dns_server
    ):
        """DNS service is available on configured listen addresses."""
        result = dns_resolve(client, "cloudflare.com", dns_server)
        assert result is not None, (
            f"DNS resolution failed from {client} via {dns_server}"
        )


class TestSshService:
    """Test SSH service accessibility."""

    def test_ssh_port_open_from_lab(self, tcp_connect, test_topology):
        """SSH port is accessible from lab networks."""
        assert tcp_connect("mgmt-client", test_topology.mgmt_gateway, 22), (
            "SSH port not accessible from mgmt-client"
        )

    def test_ssh_port_open_from_wan(self, tcp_connect, test_topology):
        """SSH port is accessible from WAN (HOME_NETWORK)."""
        assert tcp_connect("wan-client", test_topology.wan_ip, 22), (
            "SSH port not accessible from wan-client"
        )

    def test_ssh_connection_works(self, vyos_show):
        """
        SSH connection actually works.

        This implicitly tests SSH by using the vyos_show fixture
        which connects via SSH to run commands.
        """
        output = vyos_show("show version")
        assert "VyOS" in output, "SSH connection or VyOS not working properly"
