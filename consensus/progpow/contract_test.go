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

package progpow_test

import (
	"bytes"
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

// SimpleStorage contract bytecodes, hand-compiled from EVM opcodes.
//
// The contract implements two functions:
//   set(uint256 v) — stores v in storage slot 0        [selector: 0x60fe47b1]
//   get()          — returns the value in storage slot 0 [selector: 0x6d4ce63c]
//
// Runtime bytecode (50 bytes):
//   0x00: PUSH1 0x00, CALLDATALOAD, PUSH1 0xe0, SHR       — extract function selector
//   0x06: DUP1, PUSH4 0x60fe47b1, EQ, PUSH1 0x1e, JUMPI   — dispatch set()
//   0x10: PUSH4 0x6d4ce63c, EQ, PUSH1 0x26, JUMPI         — dispatch get()
//   0x19: PUSH1 0x00, PUSH1 0x00, REVERT                   — fallback: revert
//   0x1e: JUMPDEST, PUSH1 0x04, CALLDATALOAD, PUSH1 0x00, SSTORE, STOP  — set handler
//   0x26: JUMPDEST, PUSH1 0x00, SLOAD, PUSH1 0x00, MSTORE, PUSH1 0x20, PUSH1 0x00, RETURN — get handler
//
// Init code (12 bytes): CODECOPY runtime to memory, RETURN it.
var (
	simpleStorageRuntime = common.Hex2Bytes("60003560e01c806360fe47b114601e57636d4ce63c1460265760006000fd5b600435600055005b60005460005260206000f3")
	simpleStorageDeploy  = common.Hex2Bytes("6032600c60003960326000f3" + "60003560e01c806360fe47b114601e57636d4ce63c1460265760006000fd5b600435600055005b60005460005260206000f3")
)

// TestContractDeployment deploys the SimpleStorage contract via
// types.NewContractCreation and verifies:
//   - receipt status is successful
//   - contract address matches crypto.CreateAddress prediction
//   - deployed code matches the expected runtime bytecode
func TestContractDeployment(t *testing.T) {
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

	signer := types.LatestSigner(params.YottafluxChainConfig)
	contractAddr := crypto.CreateAddress(testAddress, 0)

	blocks, receipts := core.GenerateChain(params.YottafluxChainConfig, genesis, engine, db, 1, func(i int, gen *core.BlockGen) {
		gen.SetCoinbase(common.Address{0x01})
		tx, err := types.SignTx(
			types.NewContractCreation(0, big.NewInt(0), 200000, gen.BaseFee(), simpleStorageDeploy),
			signer,
			testKey,
		)
		if err != nil {
			t.Fatalf("failed to sign deploy tx: %v", err)
		}
		gen.AddTx(tx)
	})

	chain, err := core.NewBlockChain(db, nil, params.YottafluxChainConfig, engine, vm.Config{}, nil, nil)
	if err != nil {
		t.Fatalf("failed to create blockchain: %v", err)
	}
	defer chain.Stop()

	if n, err := chain.InsertChain(blocks); err != nil {
		t.Fatalf("failed to insert block %d: %v", n, err)
	}

	// Verify receipt
	if len(receipts[0]) != 1 {
		t.Fatalf("expected 1 receipt, got %d", len(receipts[0]))
	}
	if receipts[0][0].Status != types.ReceiptStatusSuccessful {
		t.Errorf("deploy receipt status = %d, want %d (success)", receipts[0][0].Status, types.ReceiptStatusSuccessful)
	}
	if receipts[0][0].ContractAddress != contractAddr {
		t.Errorf("contract address = %v, want %v", receipts[0][0].ContractAddress, contractAddr)
	}

	// Verify deployed code
	statedb, err := chain.State()
	if err != nil {
		t.Fatalf("failed to get state: %v", err)
	}
	code := statedb.GetCode(contractAddr)
	if len(code) == 0 {
		t.Fatal("contract has no code after deployment")
	}
	if !bytes.Equal(code, simpleStorageRuntime) {
		t.Errorf("deployed code mismatch:\n  got  %x\n  want %x", code, simpleStorageRuntime)
	}
}

// TestContractStorageReadWrite deploys the SimpleStorage contract in block 1,
// calls set(42) in block 2, and verifies storage slot 0 equals 42.
func TestContractStorageReadWrite(t *testing.T) {
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

	signer := types.LatestSigner(params.YottafluxChainConfig)
	contractAddr := crypto.CreateAddress(testAddress, 0)

	// Build set(42) calldata: 4-byte selector + 32-byte uint256
	setSelector := crypto.Keccak256([]byte("set(uint256)"))[:4]
	setData := append(setSelector, common.LeftPadBytes(big.NewInt(42).Bytes(), 32)...)

	// Block 0: deploy contract, Block 1: call set(42)
	blocks, receipts := core.GenerateChain(params.YottafluxChainConfig, genesis, engine, db, 2, func(i int, gen *core.BlockGen) {
		gen.SetCoinbase(common.Address{0x01})
		switch i {
		case 0:
			tx, err := types.SignTx(
				types.NewContractCreation(gen.TxNonce(testAddress), big.NewInt(0), 200000, gen.BaseFee(), simpleStorageDeploy),
				signer,
				testKey,
			)
			if err != nil {
				t.Fatalf("failed to sign deploy tx: %v", err)
			}
			gen.AddTx(tx)
		case 1:
			tx, err := types.SignTx(
				types.NewTransaction(gen.TxNonce(testAddress), contractAddr, big.NewInt(0), 100000, gen.BaseFee(), setData),
				signer,
				testKey,
			)
			if err != nil {
				t.Fatalf("failed to sign set tx: %v", err)
			}
			gen.AddTx(tx)
		}
	})

	chain, err := core.NewBlockChain(db, nil, params.YottafluxChainConfig, engine, vm.Config{}, nil, nil)
	if err != nil {
		t.Fatalf("failed to create blockchain: %v", err)
	}
	defer chain.Stop()

	if n, err := chain.InsertChain(blocks); err != nil {
		t.Fatalf("failed to insert block %d: %v", n, err)
	}

	// Verify both receipts succeeded
	for blk := 0; blk < 2; blk++ {
		if len(receipts[blk]) != 1 {
			t.Fatalf("block %d: expected 1 receipt, got %d", blk, len(receipts[blk]))
		}
		if receipts[blk][0].Status != types.ReceiptStatusSuccessful {
			t.Errorf("block %d: receipt status = %d, want success", blk, receipts[blk][0].Status)
		}
	}

	// Verify storage slot 0 = 42
	statedb, err := chain.State()
	if err != nil {
		t.Fatalf("failed to get state: %v", err)
	}
	slot0 := statedb.GetState(contractAddr, common.Hash{})
	expected := common.BigToHash(big.NewInt(42))
	if slot0 != expected {
		t.Errorf("storage slot 0 = %v, want %v", slot0, expected)
	}
}

// TestContractWithEIP1559 performs the same deploy+set flow using EIP-1559
// DynamicFeeTx transactions, verifying that contracts work correctly with
// the London fork active from block 0.
func TestContractWithEIP1559(t *testing.T) {
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

	signer := types.LatestSigner(params.YottafluxChainConfig)
	contractAddr := crypto.CreateAddress(testAddress, 0)

	setSelector := crypto.Keccak256([]byte("set(uint256)"))[:4]
	setData := append(setSelector, common.LeftPadBytes(big.NewInt(99).Bytes(), 32)...)

	blocks, receipts := core.GenerateChain(params.YottafluxChainConfig, genesis, engine, db, 2, func(i int, gen *core.BlockGen) {
		gen.SetCoinbase(common.Address{0x01})
		switch i {
		case 0: // Deploy via EIP-1559
			tx, err := types.SignTx(
				types.NewTx(&types.DynamicFeeTx{
					ChainID:   params.YottafluxChainConfig.ChainID,
					Nonce:     gen.TxNonce(testAddress),
					GasTipCap: big.NewInt(1000000000),  // 1 gwei
					GasFeeCap: big.NewInt(10000000000), // 10 gwei
					Gas:       200000,
					To:        nil, // contract creation
					Value:     big.NewInt(0),
					Data:      simpleStorageDeploy,
				}),
				signer,
				testKey,
			)
			if err != nil {
				t.Fatalf("failed to sign EIP-1559 deploy tx: %v", err)
			}
			gen.AddTx(tx)
		case 1: // Call set(99) via EIP-1559
			tx, err := types.SignTx(
				types.NewTx(&types.DynamicFeeTx{
					ChainID:   params.YottafluxChainConfig.ChainID,
					Nonce:     gen.TxNonce(testAddress),
					GasTipCap: big.NewInt(1000000000),
					GasFeeCap: big.NewInt(10000000000),
					Gas:       100000,
					To:        &contractAddr,
					Value:     big.NewInt(0),
					Data:      setData,
				}),
				signer,
				testKey,
			)
			if err != nil {
				t.Fatalf("failed to sign EIP-1559 set tx: %v", err)
			}
			gen.AddTx(tx)
		}
	})

	chain, err := core.NewBlockChain(db, nil, params.YottafluxChainConfig, engine, vm.Config{}, nil, nil)
	if err != nil {
		t.Fatalf("failed to create blockchain: %v", err)
	}
	defer chain.Stop()

	if n, err := chain.InsertChain(blocks); err != nil {
		t.Fatalf("failed to insert block %d: %v", n, err)
	}

	// Verify receipts
	for blk := 0; blk < 2; blk++ {
		if len(receipts[blk]) != 1 {
			t.Fatalf("block %d: expected 1 receipt, got %d", blk, len(receipts[blk]))
		}
		if receipts[blk][0].Status != types.ReceiptStatusSuccessful {
			t.Errorf("block %d: receipt status = %d, want success", blk, receipts[blk][0].Status)
		}
	}

	// Verify contract has code
	statedb, err := chain.State()
	if err != nil {
		t.Fatalf("failed to get state: %v", err)
	}
	code := statedb.GetCode(contractAddr)
	if len(code) == 0 {
		t.Fatal("contract has no code after EIP-1559 deployment")
	}

	// Verify storage slot 0 = 99
	slot0 := statedb.GetState(contractAddr, common.Hash{})
	expected := common.BigToHash(big.NewInt(99))
	if slot0 != expected {
		t.Errorf("storage slot 0 = %v, want %v (expected 99)", slot0, expected)
	}
}
