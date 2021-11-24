package local

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ava-labs/avalanche-network-runner/api"
	apimocks "github.com/ava-labs/avalanche-network-runner/api/mocks"
	"github.com/ava-labs/avalanche-network-runner/local/mocks"
	"github.com/ava-labs/avalanche-network-runner/network"
	"github.com/ava-labs/avalanche-network-runner/network/node"
	"github.com/ava-labs/avalanche-network-runner/utils"
	"github.com/ava-labs/avalanchego/api/health"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/staking"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/stretchr/testify/assert"
)

const (
	defaultHealthyTimeout = 5 * time.Second
)

var (
	_ NewNodeProcessF   = newMockProcessUndef
	_ NewNodeProcessF   = newMockProcessSuccessful
	_ NewNodeProcessF   = newMockProcessFailedStart
	_ api.NewAPIClientF = newMockAPISuccessful
	_ api.NewAPIClientF = newMockAPIUnhealthy
)

// Returns an API client where:
// * The Health API's Health method always returns healthy
// * The CChainEthAPI's Close method may be called
// * Only the above 2 methods may be called
// TODO have this method return an API Client that has all
// APIs and methods implemented
func newMockAPISuccessful(ipAddr string, port uint, requestTimeout time.Duration) api.Client {
	healthReply := &health.APIHealthClientReply{Healthy: true}
	healthClient := &apimocks.HealthClient{}
	healthClient.On("Health").Return(healthReply, nil)
	// ethClient used when removing nodes, to close websocket connection
	ethClient := &apimocks.EthClient{}
	ethClient.On("Close").Return()
	client := &apimocks.Client{}
	client.On("HealthAPI").Return(healthClient)
	client.On("CChainEthAPI").Return(ethClient)
	return client
}

// Returns an API client where the Health API's Health method always returns unhealthy
func newMockAPIUnhealthy(ipAddr string, port uint, requestTimeout time.Duration) api.Client {
	healthReply := &health.APIHealthClientReply{Healthy: false}
	healthClient := &apimocks.HealthClient{}
	healthClient.On("Health").Return(healthReply, nil)
	client := &apimocks.Client{}
	client.On("HealthAPI").Return(healthClient)
	return client
}

func newMockProcessUndef(node.Config, ...string) (NodeProcess, error) {
	return &mocks.NodeProcess{}, nil
}

// Returns a NodeProcess that always returns nil
func newMockProcessSuccessful(node.Config, ...string) (NodeProcess, error) {
	process := &mocks.NodeProcess{}
	process.On("Start").Return(nil)
	process.On("Wait").Return(nil)
	process.On("Stop").Return(nil)
	return process, nil
}

// Return a NodeProcess that returns an error when Start is called
func newMockProcessFailedStart(node.Config, ...string) (NodeProcess, error) {
	process := &mocks.NodeProcess{}
	process.On("Start").Return(errors.New("Start failed"))
	process.On("Wait").Return(nil)
	process.On("Stop").Return(nil)
	return process, nil
}

// Start a network with no nodes
func TestNewNetworkEmpty(t *testing.T) {
	assert := assert.New(t)
	networkConfig := defaultNetworkConfig(t)
	networkConfig.NodeConfigs = nil
	net, err := NewNetwork(
		logging.NoLog{},
		networkConfig,
		newMockAPISuccessful,
		newMockProcessUndef,
	)
	assert.NoError(err)
	// Assert that GetNodesNames() returns an empty list
	names, err := net.GetNodesNames()
	assert.NoError(err)
	assert.Len(names, 0)
}

// Start a network with one node.
func TestNewNetworkOneNode(t *testing.T) {
	assert := assert.New(t)
	networkConfig := defaultNetworkConfig(t)
	networkConfig.NodeConfigs = networkConfig.NodeConfigs[:1]
	// Assert that the node's config is being passed correctly
	// to the function that starts the node process.
	newProcessF := func(config node.Config, _ ...string) (NodeProcess, error) {
		assert.True(config.IsBeacon)
		assert.EqualValues(networkConfig.NodeConfigs[0], config)
		return newMockProcessSuccessful(config)
	}
	net, err := NewNetwork(
		logging.NoLog{},
		networkConfig,
		newMockAPISuccessful,
		newProcessF,
	)
	assert.NoError(err)

	// Assert that GetNodesNames() includes only the 1 node's name
	names, err := net.GetNodesNames()
	assert.NoError(err)
	assert.Contains(names, networkConfig.NodeConfigs[0].Name)
	assert.Len(names, 1)

	// Assert that the network's genesis was set
	assert.EqualValues(networkConfig.Genesis, net.(*localNetwork).genesis)
}

// Test that NewNetwork returns an error when
// starting a node returns an error
func TestNewNetworkFailToStartNode(t *testing.T) {
	assert := assert.New(t)
	networkConfig := defaultNetworkConfig(t)
	_, err := NewNetwork(
		logging.NoLog{},
		networkConfig,
		newMockAPISuccessful,
		newMockProcessFailedStart,
	)
	assert.Error(err)
}

// Check configs that are expected to be invalid at network creation time
func TestWrongNetworkConfigs(t *testing.T) {
	tests := map[string]struct {
		config network.Config
	}{
		"no ImplSpecificConfig": {
			config: network.Config{
				Genesis: []byte("nonempty"),
				NodeConfigs: []node.Config{
					{
						IsBeacon:    true,
						StakingKey:  []byte("nonempty"),
						StakingCert: []byte("nonempty"),
					},
				},
			},
		},
		"empty nodeID": {
			config: network.Config{
				Genesis: []byte("nonempty"),
				NodeConfigs: []node.Config{
					{
						ImplSpecificConfig: NodeConfig{
							BinaryPath: "pepe",
						},
						IsBeacon:    true,
						StakingKey:  []byte("nonempty"),
						StakingCert: []byte("nonempty"),
					},
				},
			},
		},
		"no Genesis": {
			config: network.Config{
				NodeConfigs: []node.Config{
					{
						ImplSpecificConfig: NodeConfig{
							BinaryPath: "pepe",
						},
						IsBeacon:    true,
						StakingKey:  []byte("nonempty"),
						StakingCert: []byte("nonempty"),
					},
				},
			},
		},
		"StakingKey but no StakingCert": {
			config: network.Config{
				Genesis: []byte("nonempty"),
				NodeConfigs: []node.Config{
					{
						ImplSpecificConfig: NodeConfig{
							BinaryPath: "pepe",
						},
						IsBeacon:   true,
						StakingKey: []byte("nonempty"),
					},
				},
			},
		},
		"StakingCert but no StakingKey": {
			config: network.Config{
				Genesis: []byte("nonempty"),
				NodeConfigs: []node.Config{
					{
						ImplSpecificConfig: NodeConfig{
							BinaryPath: "pepe",
						},
						IsBeacon:    true,
						StakingCert: []byte("nonempty"),
					},
				},
			},
		},
		"no beacon node": {
			config: network.Config{
				Genesis: []byte("nonempty"),
				NodeConfigs: []node.Config{
					{
						ImplSpecificConfig: NodeConfig{
							BinaryPath: "pepe",
						},
						StakingKey:  []byte("nonempty"),
						StakingCert: []byte("nonempty"),
					},
				},
			},
		},
		"repeated name": {
			config: network.Config{
				Genesis: []byte("nonempty"),
				NodeConfigs: []node.Config{
					{
						Name: "node0",
						ImplSpecificConfig: NodeConfig{
							BinaryPath: "pepe",
						},
						IsBeacon:    true,
						StakingKey:  []byte("nonempty"),
						StakingCert: []byte("nonempty"),
					},
					{
						Name: "node0",
						ImplSpecificConfig: NodeConfig{
							BinaryPath: "pepe",
						},
						IsBeacon:    true,
						StakingKey:  []byte("nonempty"),
						StakingCert: []byte("nonempty"),
					},
				},
			},
		},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			_, err := NewNetwork(logging.NoLog{}, tt.config, newMockAPISuccessful, newMockProcessSuccessful)
			assert.Error(err)
		})
	}
}

// Give incorrect type to interface{} ImplSpecificConfig
func TestImplSpecificConfigInterface(t *testing.T) {
	assert := assert.New(t)
	networkConfig := defaultNetworkConfig(t)
	networkConfig.NodeConfigs[0].ImplSpecificConfig = "should not be string"
	_, err := NewNetwork(logging.NoLog{}, networkConfig, newMockAPISuccessful, newMockProcessSuccessful)
	assert.Error(err)
}

// Assert that the network's Healthy() method returns an
// error when all nodes' Health API return unhealthy
func TestUnhealthyNetwork(t *testing.T) {
	assert := assert.New(t)
	networkConfig := defaultNetworkConfig(t)
	net, err := NewNetwork(logging.NoLog{}, networkConfig, newMockAPIUnhealthy, newMockProcessSuccessful)
	assert.NoError(err)
	assert.Error(utils.AwaitNetworkHealthy(net, defaultHealthyTimeout))
}

// Create a network without giving names to nodes.
// Checks that the generated names are the correct number and unique.
func TestGeneratedNodesNames(t *testing.T) {
	assert := assert.New(t)
	networkConfig := defaultNetworkConfig(t)
	for i := range networkConfig.NodeConfigs {
		networkConfig.NodeConfigs[i].Name = ""
	}
	net, err := NewNetwork(logging.NoLog{}, networkConfig, newMockAPISuccessful, newMockProcessSuccessful)
	assert.NoError(err)
	nodeNameMap := make(map[string]bool)
	nodeNames, err := net.GetNodesNames()
	assert.NoError(err)
	for _, nodeName := range nodeNames {
		nodeNameMap[nodeName] = true
	}
	assert.EqualValues(len(nodeNameMap), len(networkConfig.NodeConfigs))
}

// TODO add byzantine node to conf
// TestNetworkFromConfig creates/waits/checks/stops a network from config file
// the check verify that all the nodes can be accessed
func TestNetworkFromConfig(t *testing.T) {
	assert := assert.New(t)
	networkConfig := defaultNetworkConfig(t)
	net, err := NewNetwork(logging.NoLog{}, networkConfig, newMockAPISuccessful, newMockProcessSuccessful)
	assert.NoError(err)
	assert.NoError(utils.AwaitNetworkHealthy(net, defaultHealthyTimeout))
	runningNodes := make(map[string]struct{})
	for _, nodeConfig := range networkConfig.NodeConfigs {
		runningNodes[nodeConfig.Name] = struct{}{}
	}
	checkNetwork(t, net, runningNodes, nil)
}

// TestNetworkNodeOps creates an empty network,
// adds nodes one by one, then removes nodes one by one.
// Setween all operations, a network check is performed
// to verify that all the running nodes are in the network,
// and all removed nodes are not.
func TestNetworkNodeOps(t *testing.T) {
	assert := assert.New(t)

	// Start a new, empty network
	emptyNetworkConfig, err := emptyNetworkConfig()
	assert.NoError(err)
	net, err := NewNetwork(logging.NoLog{}, emptyNetworkConfig, newMockAPISuccessful, newMockProcessSuccessful)
	assert.NoError(err)
	runningNodes := make(map[string]struct{})

	// Add nodes to the network one by one
	networkConfig := defaultNetworkConfig(t)
	for _, nodeConfig := range networkConfig.NodeConfigs {
		_, err := net.AddNode(nodeConfig)
		assert.NoError(err)
		runningNodes[nodeConfig.Name] = struct{}{}
		checkNetwork(t, net, runningNodes, nil)
	}
	// Wait for all nodes to be healthy
	assert.NoError(utils.AwaitNetworkHealthy(net, defaultHealthyTimeout))

	// Remove nodes one by one
	removedNodes := make(map[string]struct{})
	for _, nodeConfig := range networkConfig.NodeConfigs {
		_, err := net.GetNode(nodeConfig.Name)
		assert.NoError(err)
		err = net.RemoveNode(nodeConfig.Name)
		assert.NoError(err)
		removedNodes[nodeConfig.Name] = struct{}{}
		delete(runningNodes, nodeConfig.Name)
		checkNetwork(t, net, runningNodes, removedNodes)
	}
}

// TestNodeNotFound checks all operations fail for an unknown node,
// being it either not created, or created and removed thereafter
func TestNodeNotFound(t *testing.T) {
	assert := assert.New(t)
	emptyNetworkConfig, err := emptyNetworkConfig()
	assert.NoError(err)
	networkConfig := defaultNetworkConfig(t)
	net, err := NewNetwork(logging.NoLog{}, emptyNetworkConfig, newMockAPISuccessful, newMockProcessSuccessful)
	assert.NoError(err)
	_, err = net.AddNode(networkConfig.NodeConfigs[0])
	assert.NoError(err)
	// get node
	_, err = net.GetNode(networkConfig.NodeConfigs[0].Name)
	assert.NoError(err)
	// get non-existent node
	_, err = net.GetNode(networkConfig.NodeConfigs[1].Name)
	assert.Error(err)
	// remove non-existent node
	err = net.RemoveNode(networkConfig.NodeConfigs[1].Name)
	assert.Error(err)
	// remove node
	err = net.RemoveNode(networkConfig.NodeConfigs[0].Name)
	assert.NoError(err)
	// get removed node
	_, err = net.GetNode(networkConfig.NodeConfigs[0].Name)
	assert.Error(err)
	// remove already-removed node
	err = net.RemoveNode(networkConfig.NodeConfigs[0].Name)
	assert.Error(err)
}

// TestStoppedNetwork checks that operations fail for an already stopped network
func TestStoppedNetwork(t *testing.T) {
	assert := assert.New(t)
	emptyNetworkConfig, err := emptyNetworkConfig()
	assert.NoError(err)
	networkConfig := defaultNetworkConfig(t)
	net, err := NewNetwork(logging.NoLog{}, emptyNetworkConfig, newMockAPISuccessful, newMockProcessSuccessful)
	assert.NoError(err)
	_, err = net.AddNode(networkConfig.NodeConfigs[0])
	assert.NoError(err)
	// first GetNodesNames should return some nodes
	_, err = net.GetNodesNames()
	assert.NoError(err)
	err = net.Stop(context.TODO())
	assert.NoError(err)
	// Stop failure
	assert.EqualValues(net.Stop(context.TODO()), network.ErrStopped)
	// AddNode failure
	_, err = net.AddNode(networkConfig.NodeConfigs[1])
	assert.EqualValues(err, network.ErrStopped)
	// GetNode failure
	_, err = net.GetNode(networkConfig.NodeConfigs[0].Name)
	assert.EqualValues(err, network.ErrStopped)
	// second GetNodesNames should return no nodes
	_, err = net.GetNodesNames()
	assert.EqualValues(err, network.ErrStopped)
	// RemoveNode failure
	assert.EqualValues(net.RemoveNode(networkConfig.NodeConfigs[0].Name), network.ErrStopped)
	// Healthy failure
	assert.EqualValues(utils.AwaitNetworkHealthy(net, defaultHealthyTimeout), network.ErrStopped)
}

// checkNetwork receives a network, a set of running nodes (started and not removed yet), and
// a set of removed nodes, checking:
// - GetNodeNames retrieves the correct number of running nodes
// - GetNode does not fail for given running nodes
// - GetNode does fail for given stopped nodes
func checkNetwork(t *testing.T, net network.Network, runningNodes map[string]struct{}, removedNodes map[string]struct{}) {
	assert := assert.New(t)
	nodeNames, err := net.GetNodesNames()
	assert.NoError(err)
	assert.EqualValues(len(nodeNames), len(runningNodes))
	for nodeName := range runningNodes {
		_, err := net.GetNode(nodeName)
		assert.NoError(err)
	}
	for nodeName := range removedNodes {
		_, err := net.GetNode(nodeName)
		assert.Error(err)
	}
}

// Return a network config that has no nodes
func emptyNetworkConfig() (network.Config, error) {
	networkID := uint32(1337)
	// Use a dummy genesis
	genesis, err := network.NewAvalancheGoGenesis(
		logging.NoLog{},
		networkID,
		[]network.AddrAndBalance{
			{
				Addr:    ids.GenerateTestShortID(),
				Balance: 1,
			},
		},
		nil,
		[]ids.ShortID{ids.GenerateTestShortID()},
	)
	if err != nil {
		return network.Config{}, err
	}
	return network.Config{
		LogLevel: "DEBUG",
		Name:     "My Network",
		Genesis:  genesis,
	}, nil
}

// Returns a config for a three node network,
// where the nodes have randomly generated staking
// kets and certificates.
func defaultNetworkConfig(t *testing.T) network.Config {
	assert := assert.New(t)
	networkConfig, err := emptyNetworkConfig()
	assert.NoError(err)
	for i := 0; i < 3; i++ {
		nodeConfig := node.Config{
			Name: fmt.Sprintf("node%d", i),
			ImplSpecificConfig: NodeConfig{
				BinaryPath: "pepito",
			},
		}
		nodeConfig.StakingCert, nodeConfig.StakingKey, err = staking.NewCertAndKeyBytes()
		assert.NoError(err)
		networkConfig.NodeConfigs = append(networkConfig.NodeConfigs, nodeConfig)
	}
	networkConfig.NodeConfigs[0].IsBeacon = true
	return networkConfig
}
