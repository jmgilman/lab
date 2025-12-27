"""
Connectivity tests for the VyOS gateway.

These tests verify that basic routing and reachability work correctly
across all network segments.
"""

import pytest


class TestGatewayReachability:
    """Test that each VLAN client can reach its gateway."""

    @pytest.mark.parametrize(
        "client,gateway",
        [
            ("mgmt-client", "10.10.10.1"),
            ("prov-client", "10.10.20.1"),
            ("platform-client", "10.10.30.1"),
            ("cluster-client", "10.10.40.1"),
            ("service-client", "10.10.50.1"),
            ("storage-client", "10.10.60.1"),
        ],
    )
    def test_vlan_client_reaches_gateway(self, ping, client, gateway):
        """Each VLAN client can ping its gateway IP."""
        assert ping(client, gateway), f"{client} cannot reach gateway {gateway}"

    def test_wan_client_reaches_gateway(self, ping, test_topology):
        """WAN client can ping the gateway's WAN interface."""
        assert ping("wan-client", test_topology.wan_ip), (
            f"wan-client cannot reach gateway WAN IP {test_topology.wan_ip}"
        )


class TestInterVlanRouting:
    """Test routing between different VLANs through the gateway."""

    def test_mgmt_to_platform(self, ping, test_topology):
        """Management client can reach platform client via gateway routing."""
        assert ping("mgmt-client", test_topology.platform_client_ip), (
            "mgmt-client cannot reach platform-client (inter-VLAN routing failure)"
        )

    def test_platform_to_mgmt(self, ping, test_topology):
        """Platform client can reach management client via gateway routing."""
        assert ping("platform-client", test_topology.mgmt_client_ip), (
            "platform-client cannot reach mgmt-client (inter-VLAN routing failure)"
        )

    def test_cluster_to_storage(self, ping, test_topology):
        """Cluster client can reach storage client via gateway routing."""
        assert ping("cluster-client", test_topology.storage_client_ip), (
            "cluster-client cannot reach storage-client (inter-VLAN routing failure)"
        )


class TestWanConnectivity:
    """Test connectivity between lab networks and WAN."""

    def test_lab_client_reaches_wan_client(self, ping, test_topology):
        """Lab client can reach WAN client (via NAT)."""
        assert ping("mgmt-client", test_topology.wan_client_ip), (
            "mgmt-client cannot reach wan-client (NAT or routing failure)"
        )

    def test_all_vlan_clients_reach_wan(self, ping, vlan_clients, test_topology):
        """All VLAN clients can reach the WAN network."""
        for client in vlan_clients:
            assert ping(client, test_topology.wan_client_ip), (
                f"{client} cannot reach wan-client"
            )
