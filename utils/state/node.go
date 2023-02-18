package state

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rocket-pool/rocketpool-go/node"
	"github.com/rocket-pool/rocketpool-go/rocketpool"
	"github.com/rocket-pool/rocketpool-go/utils/multicall"
	"golang.org/x/sync/errgroup"
)

const (
	legacyNodeBatchSize  int = 200
	nodeAddressBatchSize int = 2000
)

// Complete details for a node
type NativeNodeDetails struct {
	Exists                           bool           `abi:"exists"`
	RegistrationTime                 *big.Int       `abi:"registrationTime"`
	TimezoneLocation                 string         `abi:"timezoneLocation"`
	FeeDistributorInitialised        bool           `abi:"feeDistributorInitialised"`
	FeeDistributorAddress            common.Address `abi:"feeDistributorAddress"`
	RewardNetwork                    *big.Int       `abi:"rewardNetwork"`
	RplStake                         *big.Int       `abi:"rplStake"`
	EffectiveRPLStake                *big.Int       `abi:"effectiveRPLStake"`
	MinimumRPLStake                  *big.Int       `abi:"minimumRPLStake"`
	MaximumRPLStake                  *big.Int       `abi:"maximumRPLStake"`
	EthMatched                       *big.Int       `abi:"ethMatched"`
	EthMatchedLimit                  *big.Int       `abi:"ethMatchedLimit"`
	MinipoolCount                    *big.Int       `abi:"minipoolCount"`
	BalanceETH                       *big.Int       `abi:"balanceETH"`
	BalanceRETH                      *big.Int       `abi:"balanceRETH"`
	BalanceRPL                       *big.Int       `abi:"balanceRPL"`
	BalanceOldRPL                    *big.Int       `abi:"balanceOldRPL"`
	DepositCreditBalance             *big.Int       `abi:"depositCreditBalance"`
	DistributorBalanceUserETH        *big.Int       `abi:"distributorBalanceUserETH"`
	DistributorBalanceNodeETH        *big.Int       `abi:"distributorBalanceNodeETH"`
	WithdrawalAddress                common.Address `abi:"withdrawalAddress"`
	PendingWithdrawalAddress         common.Address `abi:"pendingWithdrawalAddress"`
	SmoothingPoolRegistrationState   bool           `abi:"smoothingPoolRegistrationState"`
	SmoothingPoolRegistrationChanged *big.Int       `abi:"smoothingPoolRegistrationChanged"`
	NodeAddress                      common.Address `abi:"nodeAddress"`
}

// Gets the details for a node using the efficient multicall contract
func GetNativeNodeDetails(rp *rocketpool.RocketPool, contracts *NetworkContracts, nodeAddress common.Address, isAtlasDeployed bool) (NativeNodeDetails, error) {
	opts := &bind.CallOpts{
		BlockNumber: contracts.ElBlockNumber,
	}
	details := NativeNodeDetails{}
	details.NodeAddress = nodeAddress

	avgFee := big.NewInt(0)
	addNodeDetailsCalls(contracts, contracts.Multicaller, &details, nodeAddress, &avgFee, isAtlasDeployed)

	_, err := contracts.Multicaller.FlexibleCall(true, opts)
	if err != nil {
		return NativeNodeDetails{}, fmt.Errorf("error executing multicall: %w", err)
	}

	// Get the node's ETH balance
	details.BalanceETH, err = rp.Client.BalanceAt(context.Background(), nodeAddress, opts.BlockNumber)
	if err != nil {
		return NativeNodeDetails{}, err
	}

	// Get the distributor balance
	distributorBalance, err := rp.Client.BalanceAt(context.Background(), details.FeeDistributorAddress, opts.BlockNumber)
	if err != nil {
		return NativeNodeDetails{}, err
	}

	// Do some postprocessing on the node data
	fixupNodeDetails(rp, &details, avgFee, distributorBalance, opts)

	return details, nil
}

// Gets the details for all nodes using the efficient multicall contract
func GetAllNativeNodeDetails(rp *rocketpool.RocketPool, contracts *NetworkContracts, isAtlasDeployed bool) ([]NativeNodeDetails, error) {
	opts := &bind.CallOpts{
		BlockNumber: contracts.ElBlockNumber,
	}

	// Get the list of node addresses
	addresses, err := getNodeAddressesFast(rp, contracts, opts)
	if err != nil {
		return nil, fmt.Errorf("error getting node addresses: %w", err)
	}
	count := len(addresses)
	nodeDetails := make([]NativeNodeDetails, count)
	avgFees := make([]*big.Int, count)

	// Sync
	var wg errgroup.Group
	wg.SetLimit(threadLimit)

	// Run the getters in batches
	for i := 0; i < count; i += legacyNodeBatchSize {
		i := i
		max := i + legacyNodeBatchSize
		if max > count {
			max = count
		}

		wg.Go(func() error {
			var err error
			mc, err := multicall.NewMultiCaller(rp.Client, contracts.Multicaller.ContractAddress)
			if err != nil {
				return err
			}
			for j := i; j < max; j++ {
				address := addresses[j]
				details := &nodeDetails[j]
				details.NodeAddress = address

				avgFees[j] = big.NewInt(0)
				addNodeDetailsCalls(contracts, mc, details, address, &avgFees[j], isAtlasDeployed)
			}
			_, err = mc.FlexibleCall(true, opts)
			if err != nil {
				return fmt.Errorf("error executing multicall: %w", err)
			}
			return nil
		})
	}

	if err := wg.Wait(); err != nil {
		return nil, fmt.Errorf("error getting node details: %w", err)
	}

	// Get the balances of the nodes
	distributorAddresses := make([]common.Address, count)
	balances, err := contracts.BalanceBatcher.GetEthBalances(addresses, opts)
	if err != nil {
		return nil, fmt.Errorf("error getting node balances: %w", err)
	}
	for i, details := range nodeDetails {
		nodeDetails[i].BalanceETH = balances[i]
		distributorAddresses[i] = details.FeeDistributorAddress
	}

	// Get the balances of the distributors
	balances, err = contracts.BalanceBatcher.GetEthBalances(distributorAddresses, opts)
	if err != nil {
		return nil, fmt.Errorf("error getting distributor balances: %w", err)
	}

	// Do some postprocessing on the node data
	for i := range nodeDetails {
		fixupNodeDetails(rp, &nodeDetails[i], avgFees[i], balances[i], opts)
	}

	return nodeDetails, nil
}

// Get all node addresses using the multicaller
func getNodeAddressesFast(rp *rocketpool.RocketPool, contracts *NetworkContracts, opts *bind.CallOpts) ([]common.Address, error) {
	// Get minipool count
	nodeCount, err := node.GetNodeCount(rp, opts)
	if err != nil {
		return []common.Address{}, err
	}

	// Sync
	var wg errgroup.Group
	wg.SetLimit(threadLimit)
	addresses := make([]common.Address, nodeCount)

	// Run the getters in batches
	count := int(nodeCount)
	for i := 0; i < count; i += nodeAddressBatchSize {
		i := i
		max := i + nodeAddressBatchSize
		if max > count {
			max = count
		}

		wg.Go(func() error {
			var err error
			mc, err := multicall.NewMultiCaller(rp.Client, contracts.Multicaller.ContractAddress)
			if err != nil {
				return err
			}
			for j := i; j < max; j++ {
				mc.AddCall(contracts.RocketNodeManager, &addresses[j], "getNodeAt", big.NewInt(int64(j)))
			}
			_, err = mc.FlexibleCall(true, opts)
			if err != nil {
				return fmt.Errorf("error executing multicall: %w", err)
			}
			return nil
		})
	}

	if err := wg.Wait(); err != nil {
		return nil, fmt.Errorf("error getting node addresses: %w", err)
	}

	return addresses, nil
}

// Add all of the calls for the node details to the multicaller
func addNodeDetailsCalls(contracts *NetworkContracts, mc *multicall.MultiCaller, details *NativeNodeDetails, address common.Address, avgFee **big.Int, isAtlasDeployed bool) {
	mc.AddCall(contracts.RocketNodeManager, &details.Exists, "getNodeExists", address)
	mc.AddCall(contracts.RocketNodeManager, &details.RegistrationTime, "getNodeRegistrationTime", address)
	mc.AddCall(contracts.RocketNodeManager, &details.TimezoneLocation, "getNodeTimezoneLocation", address)
	mc.AddCall(contracts.RocketNodeManager, &details.FeeDistributorInitialised, "getFeeDistributorInitialised", address)
	mc.AddCall(contracts.RocketNodeDistributorFactory, &details.FeeDistributorAddress, "getProxyAddress", address)
	mc.AddCall(contracts.RocketNodeManager, avgFee, "getAverageNodeFee", address)
	mc.AddCall(contracts.RocketNodeManager, &details.RewardNetwork, "getRewardNetwork", address)
	mc.AddCall(contracts.RocketNodeStaking, &details.RplStake, "getNodeRPLStake", address)
	mc.AddCall(contracts.RocketNodeStaking, &details.EffectiveRPLStake, "getNodeEffectiveRPLStake", address)
	mc.AddCall(contracts.RocketNodeStaking, &details.MinimumRPLStake, "getNodeMinimumRPLStake", address)
	mc.AddCall(contracts.RocketNodeStaking, &details.MaximumRPLStake, "getNodeMaximumRPLStake", address)
	mc.AddCall(contracts.RocketMinipoolManager, &details.MinipoolCount, "getNodeMinipoolCount", address)
	mc.AddCall(contracts.RocketTokenRETH, &details.BalanceRETH, "balanceOf", address)
	mc.AddCall(contracts.RocketTokenRPL, &details.BalanceRPL, "balanceOf", address)
	mc.AddCall(contracts.RocketTokenRPLFixedSupply, &details.BalanceOldRPL, "balanceOf", address)
	mc.AddCall(contracts.RocketStorage, &details.WithdrawalAddress, "getNodeWithdrawalAddress", address)
	mc.AddCall(contracts.RocketStorage, &details.PendingWithdrawalAddress, "getNodePendingWithdrawalAddress", address)
	mc.AddCall(contracts.RocketNodeManager, &details.SmoothingPoolRegistrationState, "getSmoothingPoolRegistrationState", address)
	mc.AddCall(contracts.RocketNodeManager, &details.SmoothingPoolRegistrationChanged, "getSmoothingPoolRegistrationChanged", address)

	if isAtlasDeployed {
		mc.AddCall(contracts.RocketNodeDeposit, &details.DepositCreditBalance, "getNodeDepositCredit", address)
	}
}

// Fixes a legacy node details struct with supplemental logic
func fixupNodeDetails(rp *rocketpool.RocketPool, details *NativeNodeDetails, avgFee *big.Int, distributorBalance *big.Int, opts *bind.CallOpts) error {
	// Fix the effective stake
	if details.EffectiveRPLStake.Cmp(details.MinimumRPLStake) == -1 {
		details.EffectiveRPLStake.SetUint64(0)
	}

	// Get the user and node portions of the distributor balance
	if distributorBalance.Cmp(zero) > 0 {
		halfBalance := big.NewInt(0)
		halfBalance.Div(distributorBalance, two)
		if details.MinipoolCount.Cmp(zero) == 0 {
			// Split it 50/50 if there are no minipools
			details.DistributorBalanceNodeETH = big.NewInt(0).Set(halfBalance)
			details.DistributorBalanceUserETH = big.NewInt(0).Sub(distributorBalance, halfBalance)
		} else {
			nodeShare := big.NewInt(0)
			nodeShare.Mul(halfBalance, avgFee)
			nodeShare.Div(nodeShare, oneInWei)
			nodeShare.Add(nodeShare, halfBalance)
			details.DistributorBalanceNodeETH = nodeShare
			userShare := big.NewInt(0)
			userShare.Sub(distributorBalance, nodeShare)
			details.DistributorBalanceUserETH = userShare
		}
	} else {
		details.DistributorBalanceNodeETH = big.NewInt(0)
		details.DistributorBalanceUserETH = big.NewInt(0)
	}

	return nil
}
