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
            ("30", "10.10.30.1"),
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


class TestRoutingState:
    """Test routing table state."""

    def test_default_route_present(self, vyos_show, test_topology):
        """Default route exists via WAN gateway (CCR2004 on transit link)."""
        output = vyos_show("show ip route 0.0.0.0/0")
        assert test_topology.wan_gateway in output, (
            f"Default route via {test_topology.wan_gateway} not found"
        )

    def test_home_network_route_present(self, vyos_show, test_topology):
        """Static route to home network exists via transit link."""
        output = vyos_show(f"show ip route {test_topology.home_cidr}")
        assert test_topology.wan_gateway in output, (
            f"Route to home network {test_topology.home_cidr} via "
            f"{test_topology.wan_gateway} not found"
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
