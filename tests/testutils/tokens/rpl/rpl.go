package rpl

import (
	"fmt"
	"math/big"

	"github.com/RedDuck-Software/poolsea-go/rocketpool"
	"github.com/RedDuck-Software/poolsea-go/tokens"

	"github.com/RedDuck-Software/poolsea-go/tests/testutils/accounts"
)

// Mint an amount of RPL to an account
func MintRPL(rp *rocketpool.RocketPool, ownerAccount *accounts.Account, toAccount *accounts.Account, amount *big.Int) error {

	// Get RPL token contract address
	rocketTokenRPLAddress, err := rp.GetAddress("poolseaTokenRPL")
	if err != nil {
		return err
	}

	// Mint, approve & swap fixed-supply RPL
	if err := MintFixedSupplyRPL(rp, ownerAccount, toAccount, amount); err != nil {
		return err
	}
	if _, err := tokens.ApproveFixedSupplyRPL(rp, *rocketTokenRPLAddress, amount, toAccount.GetTransactor()); err != nil {
		return err
	}
	if _, err := tokens.SwapFixedSupplyRPLForRPL(rp, amount, toAccount.GetTransactor()); err != nil {
		return err
	}

	// Return
	return nil

}

// Mint an amount of fixed-supply RPL to an account
func MintFixedSupplyRPL(rp *rocketpool.RocketPool, ownerAccount *accounts.Account, toAccount *accounts.Account, amount *big.Int) error {
	rocketTokenFixedSupplyRPL, err := rp.GetContract("poolseaTokenRPLFixedSupply")
	if err != nil {
		return err
	}
	if _, err := rocketTokenFixedSupplyRPL.Transact(ownerAccount.GetTransactor(), "mint", toAccount.Address, amount); err != nil {
		return fmt.Errorf("Could not mint fixed-supply RPL tokens to %s: %w", toAccount.Address.Hex(), err)
	}
	return nil
}
