// Copyright 2025 The Yottaflux Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

// Package progpow_test contains integration tests that validate the full
// Yottaflux block processing pipeline using progpow.NewFaker() as the
// consensus engine and params.YottafluxChainConfig as the chain config.
package progpow_test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/progpow"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
)

// testKey is a pre-generated private key for testing.
var (
	testKey, _  = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	testAddress = crypto.PubkeyToAddress(testKey.PublicKey)
)

// TestBlockchainWithProgpowFaker creates a blockchain using YottafluxChainConfig
// and progpow.NewFaker(), generates 10 blocks, inserts them, and verifies
// the chain head and coinbase rewards.
func TestBlockchainWithProgpowFaker(t *testing.T) {
	var (
		db      = rawdb.NewMemoryDatabase()
		engine  = progpow.NewFaker()
		gspec   = &core.Genesis{
			Config:    params.YottafluxChainConfig,
			GasLimit:  30000000,
			Alloc:     core.GenesisAlloc{},
		}
		genesis = gspec.MustCommit(db)
	)
	coinbase := common.Address{0x01}

	// Generate 10 blocks
	blocks, _ := core.GenerateChain(params.YottafluxChainConfig, genesis, engine, db, 10, func(i int, gen *core.BlockGen) {
		gen.SetCoinbase(coinbase)
	})

	// Create blockchain and insert blocks
	chain, err := core.NewBlockChain(db, nil, params.YottafluxChainConfig, engine, vm.Config{}, nil, nil)
	if err != nil {
		t.Fatalf("failed to create blockchain: %v", err)
	}
	defer chain.Stop()

	if n, err := chain.InsertChain(blocks); err != nil {
		t.Fatalf("failed to insert block %d: %v", n, err)
	}

	// Verify chain head
	head := chain.CurrentBlock()
	if head.NumberU64() != 10 {
		t.Errorf("expected head block number 10, got %d", head.NumberU64())
	}

	// Verify coinbase has received block rewards.
	// Blocks 1-10 are all in early bonus period (2x) and year 1 (70% to miner).
	// Per-block base reward = 4708 YFX * 2 (early bonus) = 9416 YFX
	// Miner share = 9416 * 70% = 6591.2 YFX per block
	// Total for 10 blocks = 65912 YFX
	statedb, err := chain.State()
	if err != nil {
		t.Fatalf("failed to get state: %v", err)
	}
	balance := statedb.GetBalance(coinbase)

	// Compute expected: for each block, CalcBlockReward * 70 / 100
	expectedTotal := new(big.Int)
	for i := int64(1); i <= 10; i++ {
		reward := progpow.CalcBlockReward(big.NewInt(i))
		minerShare := new(big.Int).Mul(reward, big.NewInt(70))
		minerShare.Div(minerShare, big.NewInt(100))
		expectedTotal.Add(expectedTotal, minerShare)
	}
	if balance.Cmp(expectedTotal) != 0 {
		t.Errorf("coinbase balance = %v, want %v", balance, expectedTotal)
	}
}

// TestTransactionLifecycle pre-funds an account, generates a block with a
// value transfer, and verifies the receipt and destination balance.
func TestTransactionLifecycle(t *testing.T) {
	var (
		db     = rawdb.NewMemoryDatabase()
		engine = progpow.NewFaker()
		gspec  = &core.Genesis{
			Config:   params.YottafluxChainConfig,
			GasLimit: 30000000,
			Alloc: core.GenesisAlloc{
				testAddress: {Balance: new(big.Int).Mul(big.NewInt(1000), big.NewInt(params.Ether))},
			},
		}
		genesis = gspec.MustCommit(db)
	)

	recipient := common.Address{0xaa}
	transferAmount := big.NewInt(1000000000000000000) // 1 YFX

	// Create chain and insert blocks
	chain, err := core.NewBlockChain(db, nil, params.YottafluxChainConfig, engine, vm.Config{}, nil, nil)
	if err != nil {
		t.Fatalf("failed to create blockchain: %v", err)
	}
	defer chain.Stop()

	// Generate 1 block with a value transfer
	signer := types.LatestSigner(params.YottafluxChainConfig)
	blocks, receipts := core.GenerateChain(params.YottafluxChainConfig, genesis, engine, db, 1, func(i int, gen *core.BlockGen) {
		gen.SetCoinbase(common.Address{0x01})
		tx, err := types.SignTx(
			types.NewTransaction(0, recipient, transferAmount, 21000, gen.BaseFee(), nil),
			signer,
			testKey,
		)
		if err != nil {
			t.Fatalf("failed to sign tx: %v", err)
		}
		gen.AddTx(tx)
	})

	if n, err := chain.InsertChain(blocks); err != nil {
		t.Fatalf("failed to insert block %d: %v", n, err)
	}

	// Verify receipt
	if len(receipts[0]) != 1 {
		t.Fatalf("expected 1 receipt, got %d", len(receipts[0]))
	}
	if receipts[0][0].Status != types.ReceiptStatusSuccessful {
		t.Errorf("receipt status = %d, want %d (success)", receipts[0][0].Status, types.ReceiptStatusSuccessful)
	}

	// Verify recipient balance
	statedb, err := chain.State()
	if err != nil {
		t.Fatalf("failed to get state: %v", err)
	}
	balance := statedb.GetBalance(recipient)
	if balance.Cmp(transferAmount) != 0 {
		t.Errorf("recipient balance = %v, want %v", balance, transferAmount)
	}
}

// TestEIP1559TransactionWithProgpow tests an EIP-1559 dynamic fee transaction
// on the Yottaflux chain where London is active at block 0.
func TestEIP1559TransactionWithProgpow(t *testing.T) {
	var (
		db     = rawdb.NewMemoryDatabase()
		engine = progpow.NewFaker()
		gspec  = &core.Genesis{
			Config:   params.YottafluxChainConfig,
			GasLimit: 30000000,
			BaseFee:  big.NewInt(params.InitialBaseFee),
			Alloc: core.GenesisAlloc{
				testAddress: {Balance: new(big.Int).Mul(big.NewInt(1000), big.NewInt(params.Ether))},
			},
		}
		genesis = gspec.MustCommit(db)
	)

	recipient := common.Address{0xbb}
	transferAmount := big.NewInt(1000000000000000000) // 1 YFX

	chain, err := core.NewBlockChain(db, nil, params.YottafluxChainConfig, engine, vm.Config{}, nil, nil)
	if err != nil {
		t.Fatalf("failed to create blockchain: %v", err)
	}
	defer chain.Stop()

	signer := types.LatestSigner(params.YottafluxChainConfig)
	blocks, receipts := core.GenerateChain(params.YottafluxChainConfig, genesis, engine, db, 1, func(i int, gen *core.BlockGen) {
		gen.SetCoinbase(common.Address{0x01})

		// EIP-1559 dynamic fee transaction
		tx, err := types.SignTx(
			types.NewTx(&types.DynamicFeeTx{
				ChainID:   params.YottafluxChainConfig.ChainID,
				Nonce:     0,
				GasTipCap: big.NewInt(1000000000),  // 1 gwei tip
				GasFeeCap: big.NewInt(10000000000), // 10 gwei max fee
				Gas:       21000,
				To:        &recipient,
				Value:     transferAmount,
			}),
			signer,
			testKey,
		)
		if err != nil {
			t.Fatalf("failed to sign EIP-1559 tx: %v", err)
		}
		gen.AddTx(tx)
	})

	if n, err := chain.InsertChain(blocks); err != nil {
		t.Fatalf("failed to insert block %d: %v", n, err)
	}

	// Verify receipt indicates success
	if len(receipts[0]) != 1 {
		t.Fatalf("expected 1 receipt, got %d", len(receipts[0]))
	}
	if receipts[0][0].Status != types.ReceiptStatusSuccessful {
		t.Errorf("receipt status = %d, want %d (success)", receipts[0][0].Status, types.ReceiptStatusSuccessful)
	}

	// Verify that the block has a non-nil base fee (London fork is active)
	block := chain.GetBlockByNumber(1)
	if block == nil {
		t.Fatal("block 1 not found")
	}
	if block.BaseFee() == nil {
		t.Error("expected non-nil base fee (London active at block 0)")
	}

	// Verify recipient received the transfer
	statedb, err := chain.State()
	if err != nil {
		t.Fatalf("failed to get state: %v", err)
	}
	balance := statedb.GetBalance(recipient)
	if balance.Cmp(transferAmount) != 0 {
		t.Errorf("recipient balance = %v, want %v", balance, transferAmount)
	}
}

// TestProgpowChainID verifies the Yottaflux chain ID is correctly set to 7847.
func TestProgpowChainID(t *testing.T) {
	expected := big.NewInt(7847)
	if params.YottafluxChainConfig.ChainID.Cmp(expected) != 0 {
		t.Errorf("YottafluxChainConfig.ChainID = %v, want %v", params.YottafluxChainConfig.ChainID, expected)
	}
}

// TestProgpowConsensusConfig verifies the chain config has ProgPow set.
func TestProgpowConsensusConfig(t *testing.T) {
	if params.YottafluxChainConfig.ProgPow == nil {
		t.Fatal("YottafluxChainConfig.ProgPow is nil")
	}
	if params.YottafluxChainConfig.Ethash != nil {
		t.Error("YottafluxChainConfig should not have Ethash set")
	}
	if params.YottafluxChainConfig.Clique != nil {
		t.Error("YottafluxChainConfig should not have Clique set")
	}
}
