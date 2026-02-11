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

package progpow

import (
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
)

// TestTestMode tests that ProgPow works correctly in test mode.
// It seals a block and then verifies the seal.
func TestTestMode(t *testing.T) {
	header := &types.Header{Number: big.NewInt(1), Difficulty: big.NewInt(100)}

	pp := NewTester(nil, false)
	defer pp.Close()

	results := make(chan *types.Block)
	err := pp.Seal(nil, types.NewBlockWithHeader(header), results, nil)
	if err != nil {
		t.Fatalf("failed to seal block: %v", err)
	}
	select {
	case block := <-results:
		header.Nonce = types.EncodeNonce(block.Nonce())
		header.MixDigest = block.MixDigest()
		if err := pp.verifySeal(nil, header, false); err != nil {
			t.Fatalf("unexpected verification error: %v", err)
		}
	case <-time.NewTimer(60 * time.Second).C:
		t.Fatal("sealing result timeout")
	}
}

// TestRemoteSealer tests the remote sealing API:
// - GetWork returns errNoMiningWork when idle
// - After Seal, GetWork returns the correct sealhash
// - Fake SubmitWork returns false
func TestRemoteSealer(t *testing.T) {
	pp := NewTester(nil, false)
	defer pp.Close()

	api := &API{pp}
	if _, err := api.GetWork(); err != errNoMiningWork {
		t.Error("expect to return an error indicating there is no mining work")
	}
	header := &types.Header{Number: big.NewInt(1), Difficulty: big.NewInt(100)}
	block := types.NewBlockWithHeader(header)
	sealhash := pp.SealHash(header)

	// Push new work.
	results := make(chan *types.Block)
	pp.Seal(nil, block, results, nil)

	var (
		work [4]string
		err  error
	)
	if work, err = api.GetWork(); err != nil || work[0] != sealhash.Hex() {
		t.Error("expect to return a mining work with same hash")
	}

	if res := api.SubmitWork(types.BlockNonce{}, sealhash, common.Hash{}); res {
		t.Error("expect to return false when submit a fake solution")
	}

	// Push new block with same block number to replace the original one.
	header = &types.Header{Number: big.NewInt(1), Difficulty: big.NewInt(1000)}
	block = types.NewBlockWithHeader(header)
	sealhash = pp.SealHash(header)
	pp.Seal(nil, block, results, nil)

	if work, err = api.GetWork(); err != nil || work[0] != sealhash.Hex() {
		t.Error("expect to return the latest pushed work")
	}
}

// TestHashrate tests that submitted hashrates are correctly aggregated.
func TestHashrate(t *testing.T) {
	var (
		hashrates = []hexutil.Uint64{100, 200, 300}
		expect    uint64
		ids       = []common.Hash{common.HexToHash("a"), common.HexToHash("b"), common.HexToHash("c")}
	)
	pp := NewTester(nil, false)
	defer pp.Close()

	if tot := pp.Hashrate(); tot != 0 {
		t.Error("expect the result should be zero")
	}

	api := &API{pp}
	for i := 0; i < len(hashrates); i++ {
		if res := api.SubmitHashrate(hashrates[i], ids[i]); !res {
			t.Error("remote miner submit hashrate failed")
		}
		expect += uint64(hashrates[i])
	}
	if tot := pp.Hashrate(); tot != float64(expect) {
		t.Error("expect total hashrate should be same")
	}
}

// TestClosedRemoteSealer tests that operations on a closed sealer return errors.
func TestClosedRemoteSealer(t *testing.T) {
	pp := NewTester(nil, false)
	time.Sleep(1 * time.Second) // ensure exit channel is listening
	pp.Close()

	api := &API{pp}
	if _, err := api.GetWork(); err != errProgpowStopped {
		t.Error("expect to return an error to indicate progpow is stopped")
	}

	if res := api.SubmitHashrate(hexutil.Uint64(100), common.HexToHash("a")); res {
		t.Error("expect to return false when submit hashrate to a stopped progpow")
	}
}

// TestFakeModeSeal tests that fake mode returns a sealed block immediately
// with zero nonce and empty mix digest.
func TestFakeModeSeal(t *testing.T) {
	pp := NewFaker()
	header := &types.Header{Number: big.NewInt(1), Difficulty: big.NewInt(100)}
	block := types.NewBlockWithHeader(header)

	results := make(chan *types.Block, 1)
	err := pp.Seal(nil, block, results, nil)
	if err != nil {
		t.Fatalf("failed to seal block in fake mode: %v", err)
	}

	select {
	case sealed := <-results:
		if sealed.Nonce() != 0 {
			t.Errorf("expected zero nonce in fake mode, got %d", sealed.Nonce())
		}
		if sealed.MixDigest() != (common.Hash{}) {
			t.Errorf("expected empty mix digest in fake mode, got %v", sealed.MixDigest())
		}
	case <-time.NewTimer(5 * time.Second).C:
		t.Fatal("fake mode sealing timeout")
	}
}

// TestSealHash tests that SealHash produces consistent, non-zero output
// and that different headers produce different hashes.
func TestSealHash(t *testing.T) {
	pp := NewFaker()

	h1 := &types.Header{Number: big.NewInt(1), Difficulty: big.NewInt(100)}
	h2 := &types.Header{Number: big.NewInt(2), Difficulty: big.NewInt(100)}

	hash1 := pp.SealHash(h1)
	hash2 := pp.SealHash(h2)

	if hash1 == (common.Hash{}) {
		t.Error("SealHash returned zero hash")
	}
	if hash1 == hash2 {
		t.Error("different headers produced same SealHash")
	}
	// Determinism check
	if pp.SealHash(h1) != hash1 {
		t.Error("SealHash not deterministic")
	}
}
