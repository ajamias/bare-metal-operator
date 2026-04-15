package controller

import (
	"context"
	"fmt"
	"os"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/extensions/layer3/routers"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/networks"
	"github.com/gophercloud/gophercloud/v2/openstack/networking/v2/subnets"
	"github.com/gophercloud/utils/v2/openstack/clientconfig"
)

const (
	defaultSubnetCIDR          = "192.168.50.0/22"
	defaultGatewayIP           = "192.168.50.1"
	defaultAllocationPoolStart = "192.168.50.10"
	defaultAllocationPoolEnd   = "192.168.50.200"
	defaultExternalNetwork     = "external"
	defaultDNSNameserver       = "8.8.8.8"
)

// OpenstackClient wraps the gophercloud client for networking operations
type OpenstackClient struct {
	networkClient *gophercloud.ServiceClient
}

// NewOpenstackClient creates a new OpenStack client using clouds.yaml
func NewOpenstackClient() (*OpenstackClient, error) {
	// Use clouds.yaml configuration
	cloudName := os.Getenv("OS_CLOUD")
	if cloudName == "" {
		cloudName = "ajamias" // default cloud name
	}

	clientOpts := &clientconfig.ClientOpts{
		Cloud: cloudName,
	}

	provider, err := clientconfig.AuthenticatedClient(context.Background(), clientOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to create authenticated client: %w", err)
	}

	networkClient, err := openstack.NewNetworkV2(provider, gophercloud.EndpointOpts{})
	if err != nil {
		return nil, fmt.Errorf("failed to create network client: %w", err)
	}

	return &OpenstackClient{
		networkClient: networkClient,
	}, nil
}

// NetworkResources holds the IDs of created network resources
type NetworkResources struct {
	NetworkID string
	SubnetID  string
	RouterID  string
}

// CreateNetworkInfrastructure creates a network, subnet, and router for a BareMetalPool
func (c *OpenstackClient) CreateNetworkInfrastructure(poolName string, poolUID string) (*NetworkResources, error) {
	networkName := fmt.Sprintf("%s-network", poolName)
	subnetName := fmt.Sprintf("%s-subnet", poolName)
	routerName := fmt.Sprintf("%s-router", poolName)

	// Create network
	createNetworkOpts := networks.CreateOpts{
		Name:         networkName,
		AdminStateUp: gophercloud.Enabled,
	}
	network, err := networks.Create(context.Background(), c.networkClient, createNetworkOpts).Extract()
	if err != nil {
		return nil, fmt.Errorf("failed to create network: %w", err)
	}

	// Create subnet
	allocationPools := []subnets.AllocationPool{
		{
			Start: defaultAllocationPoolStart,
			End:   defaultAllocationPoolEnd,
		},
	}
	dnsNameservers := []string{defaultDNSNameserver}
	gatewayIP := defaultGatewayIP

	createSubnetOpts := subnets.CreateOpts{
		NetworkID:       network.ID,
		CIDR:            defaultSubnetCIDR,
		Name:            subnetName,
		IPVersion:       gophercloud.IPv4,
		GatewayIP:       &gatewayIP,
		AllocationPools: allocationPools,
		DNSNameservers:  dnsNameservers,
	}
	subnet, err := subnets.Create(context.Background(), c.networkClient, createSubnetOpts).Extract()
	if err != nil {
		// Clean up network if subnet creation fails
		_ = networks.Delete(context.Background(), c.networkClient, network.ID).ExtractErr()
		return nil, fmt.Errorf("failed to create subnet: %w", err)
	}

	// Create router
	createRouterOpts := routers.CreateOpts{
		Name:         routerName,
		AdminStateUp: gophercloud.Enabled,
		GatewayInfo: &routers.GatewayInfo{
			NetworkID: getExternalNetworkID(c.networkClient),
		},
	}
	router, err := routers.Create(context.Background(), c.networkClient, createRouterOpts).Extract()
	if err != nil {
		// Clean up subnet and network if router creation fails
		_ = subnets.Delete(context.Background(), c.networkClient, subnet.ID).ExtractErr()
		_ = networks.Delete(context.Background(), c.networkClient, network.ID).ExtractErr()
		return nil, fmt.Errorf("failed to create router: %w", err)
	}

	// Add subnet to router
	_, err = routers.AddInterface(context.Background(), c.networkClient, router.ID, routers.AddInterfaceOpts{
		SubnetID: subnet.ID,
	}).Extract()
	if err != nil {
		// Clean up router, subnet, and network if interface addition fails
		_ = routers.Delete(context.Background(), c.networkClient, router.ID).ExtractErr()
		_ = subnets.Delete(context.Background(), c.networkClient, subnet.ID).ExtractErr()
		_ = networks.Delete(context.Background(), c.networkClient, network.ID).ExtractErr()
		return nil, fmt.Errorf("failed to add subnet to router: %w", err)
	}

	return &NetworkResources{
		NetworkID: network.ID,
		SubnetID:  subnet.ID,
		RouterID:  router.ID,
	}, nil
}

// DeleteNetworkInfrastructure deletes the router, subnet, and network for a BareMetalPool
func (c *OpenstackClient) DeleteNetworkInfrastructure(poolName string) error {
	routerName := fmt.Sprintf("%s-router", poolName)
	subnetName := fmt.Sprintf("%s-subnet", poolName)
	networkName := fmt.Sprintf("%s-network", poolName)

	// Find router by name
	router, err := c.findRouterByName(routerName)
	if err == nil && router != nil {
		// Remove all interfaces from router
		subnetID := c.findSubnetIDByName(subnetName)
		if subnetID != "" {
			_, _ = routers.RemoveInterface(context.Background(), c.networkClient, router.ID, routers.RemoveInterfaceOpts{
				SubnetID: subnetID,
			}).Extract()
		}

		// Delete router
		_ = routers.Delete(context.Background(), c.networkClient, router.ID).ExtractErr()
	}

	// Find and delete subnet
	subnet, err := c.findSubnetByName(subnetName)
	if err == nil && subnet != nil {
		_ = subnets.Delete(context.Background(), c.networkClient, subnet.ID).ExtractErr()
	}

	// Find and delete network
	network, err := c.findNetworkByName(networkName)
	if err == nil && network != nil {
		_ = networks.Delete(context.Background(), c.networkClient, network.ID).ExtractErr()
	}

	return nil
}

// Helper functions to find resources by name
func (c *OpenstackClient) findNetworkByName(name string) (*networks.Network, error) {
	listOpts := networks.ListOpts{Name: name}
	allPages, err := networks.List(c.networkClient, listOpts).AllPages(context.Background())
	if err != nil {
		return nil, err
	}
	allNetworks, err := networks.ExtractNetworks(allPages)
	if err != nil {
		return nil, err
	}
	if len(allNetworks) == 0 {
		return nil, fmt.Errorf("network not found: %s", name)
	}
	return &allNetworks[0], nil
}

func (c *OpenstackClient) findSubnetByName(name string) (*subnets.Subnet, error) {
	listOpts := subnets.ListOpts{Name: name}
	allPages, err := subnets.List(c.networkClient, listOpts).AllPages(context.Background())
	if err != nil {
		return nil, err
	}
	allSubnets, err := subnets.ExtractSubnets(allPages)
	if err != nil {
		return nil, err
	}
	if len(allSubnets) == 0 {
		return nil, fmt.Errorf("subnet not found: %s", name)
	}
	return &allSubnets[0], nil
}

func (c *OpenstackClient) findSubnetIDByName(name string) string {
	subnet, err := c.findSubnetByName(name)
	if err != nil || subnet == nil {
		return ""
	}
	return subnet.ID
}

func (c *OpenstackClient) findRouterByName(name string) (*routers.Router, error) {
	listOpts := routers.ListOpts{Name: name}
	allPages, err := routers.List(c.networkClient, listOpts).AllPages(context.Background())
	if err != nil {
		return nil, err
	}
	allRouters, err := routers.ExtractRouters(allPages)
	if err != nil {
		return nil, err
	}
	if len(allRouters) == 0 {
		return nil, fmt.Errorf("router not found: %s", name)
	}
	return &allRouters[0], nil
}

func getExternalNetworkID(client *gophercloud.ServiceClient) string {
	// Try to find external network by name
	listOpts := networks.ListOpts{Name: defaultExternalNetwork}
	allPages, err := networks.List(client, listOpts).AllPages(context.Background())
	if err != nil {
		return ""
	}
	allNetworks, err := networks.ExtractNetworks(allPages)
	if err != nil || len(allNetworks) == 0 {
		return ""
	}
	return allNetworks[0].ID
}
