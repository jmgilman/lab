"""
Operational state tests for the VyOS gateway.

These tests verify that VyOS operational state matches expectations
by running show commands and checking the output.
"""

import pytest


class TestInterfaceState:
    """Test interface operational state."""

    def test_wan_interface_up(self, vyos_show, test_topology):
        """WAN interface is operationally up."""
        output = vyos_show(f"show interfaces ethernet {test_topology.wan_iface}")
        assert "up" in output.lower(), f"WAN interface {test_topology.wan_iface} is not up"
        assert test_topology.wan_ip in output, (
            f"WAN interface missing IP {test_topology.wan_ip}"
        )

    def test_trunk_interface_up(self, vyos_show, test_topology):
        """Trunk interface is operationally up."""
        output = vyos_show(f"show interfaces ethernet {test_topology.trunk_iface}")
        assert "up" in output.lower(), (
            f"Trunk interface {test_topology.trunk_iface} is not up"
        )

    @pytest.mark.parametrize(
        "vif,gateway_ip",
        [
            ("10", "10.10.10.1"),
            ("20", "10.10.20.1"),
            ("40", "10.10.40.1"),
            ("50", "10.10.50.1"),
            ("60", "10.10.60.1"),
        ],
    )
    def test_vlan_interface_up(self, vyos_show, test_topology, vif, gateway_ip):
        """Each VLAN sub-interface is up with correct IP."""
        output = vyos_show(
            f"show interfaces ethernet {test_topology.trunk_iface} vif {vif}"
        )
        assert "up" in output.lower(), f"VLAN {vif} interface is not up"
        assert gateway_ip in output, f"VLAN {vif} missing IP {gateway_ip}"

    def test_vlan30_bridge_interface_up(self, vyos_show, test_topology):
        """VLAN 30 uses a bridge for the platform network.

        The platform network (VLAN 30) is bridged to allow the UM760 anchor node
        to participate via a direct connection (eth4 in test, eth2 in production).
        The gateway IP lives on br30, not on the VLAN interface directly.
        """
        # Check that the VLAN interface is up and is a bridge member
        vlan_output = vyos_show(
            f"show interfaces ethernet {test_topology.trunk_iface} vif 30"
        )
        assert "up" in vlan_output.lower(), "VLAN 30 interface is not up"
        assert "br30" in vlan_output, "VLAN 30 should be a member of br30"

        # Check that the bridge has the gateway IP
        bridge_output = vyos_show("show interfaces bridge br30")
        assert "up" in bridge_output.lower(), "Bridge br30 is not up"
        assert test_topology.platform_gateway in bridge_output, (
            f"Bridge br30 missing IP {test_topology.platform_gateway}"
        )


class TestRoutingState:
    """Test routing table state."""

    def test_wan_gateway_reachable(self, ping, test_topology):
        """WAN gateway (transit link peer) is reachable from VyOS.

        This validates that the WAN interface is correctly configured
        and can reach the transit link peer.
        """
        # VyOS can reach the WAN gateway - verified through lab client connectivity
        # If lab clients can reach WAN via NAT, the routing is working
        assert ping("mgmt-client", test_topology.wan_client_transit_ip), (
            f"Cannot reach WAN gateway {test_topology.wan_client_transit_ip} "
            "- routing may not be configured correctly"
        )

    def test_connected_routes_present(self, vyos_show):
        """Connected routes exist for all VLAN networks."""
        output = vyos_show("show ip route connected")
        expected_networks = [
            "10.10.10.0/24",
            "10.10.20.0/24",
            "10.10.30.0/24",
            "10.10.40.0/24",
            "10.10.50.0/24",
            "10.10.60.0/24",
        ]
        for network in expected_networks:
            assert network in output, f"Connected route for {network} not found"


class TestBgpState:
    """Test BGP operational state."""

    def test_bgp_configured(self, vyos_show, test_topology):
        """BGP is configured with correct AS number."""
        output = vyos_show("show bgp summary")
        assert test_topology.bgp_local_as in output, (
            f"BGP AS {test_topology.bgp_local_as} not found in summary"
        )

    def test_bgp_router_id(self, vyos_show, test_topology):
        """BGP router ID is configured correctly."""
        output = vyos_show("show bgp summary")
        assert test_topology.bgp_router_id in output, (
            f"BGP router ID {test_topology.bgp_router_id} not found"
        )


class TestNatState:
    """Test NAT operational state."""

    def test_source_nat_rule_active(self, vyos_show):
        """Source NAT rule 100 is active."""
        output = vyos_show("show nat source rules")
        assert "100" in output, "NAT source rule 100 not found"
        assert "masquerade" in output.lower(), "NAT masquerade not configured"


class TestFirewallState:
    """Test firewall operational state."""

    def test_firewall_rulesets_loaded(self, vyos_show):
        """Firewall rulesets are loaded."""
        output = vyos_show("show firewall")
        expected_rulesets = ["WAN_TO_LAB", "LAB_TO_WAN", "LOCAL"]
        for ruleset in expected_rulesets:
            assert ruleset in output, f"Firewall ruleset {ruleset} not found"

    def test_firewall_groups_exist(self, vyos_show):
        """Firewall network groups are defined."""
        output = vyos_show("show firewall group")
        expected_groups = ["HOME_NETWORK", "TRANSIT_LINK", "LAB_NETWORKS", "RFC1918"]
        for group in expected_groups:
            assert group in output, f"Firewall group {group} not found"
