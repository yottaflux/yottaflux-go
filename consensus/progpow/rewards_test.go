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

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// TestCalcBlockReward tests the reward calculation at key block numbers.
func TestCalcBlockReward(t *testing.T) {
	flux := big.NewInt(params.Flux)

	tests := []struct {
		name     string
		blockNum uint64
		expected *big.Int
	}{
		{
			name:     "block 0 (early bonus)",
			blockNum: 0,
			// 4708 * 2 = 9416 YFX
			expected: new(big.Int).Mul(big.NewInt(9416), flux),
		},
		{
			name:     "block 1 (early bonus)",
			blockNum: 1,
			expected: new(big.Int).Mul(big.NewInt(9416), flux),
		},
		{
			name:     "block 149999 (last early bonus block)",
			blockNum: 149999,
			expected: new(big.Int).Mul(big.NewInt(9416), flux),
		},
		{
			name:     "block 150000 (first non-early-bonus block, still year 1 era 0)",
			blockNum: 150000,
			// 4708 YFX (no 2x bonus, no halving yet)
			expected: new(big.Int).Mul(big.NewInt(4708), flux),
		},
		{
			name:     "block 2102399 (last block of year 1, era 0)",
			blockNum: 2102399,
			expected: new(big.Int).Mul(big.NewInt(4708), flux),
		},
		{
			name:     "block 2102400 (first block of year 2, era 1 = first halving)",
			blockNum: 2102400,
			// 4708 / 2 = 2354 YFX
			expected: new(big.Int).Mul(big.NewInt(2354), flux),
		},
		{
			name:     "block 4204800 (year 3, era 2 = second halving)",
			blockNum: 4204800,
			// 4708 / 4 = 1177 YFX
			expected: new(big.Int).Mul(big.NewInt(1177), flux),
		},
		{
			name:     "block 42048000 (tail emission starts at 20 * BlocksPerYear)",
			blockNum: 42048000,
			// TailEmissionPerBlock: 105,000,000 * Flux / BlocksPerYear
			expected: new(big.Int).Set(TailEmissionPerBlock),
		},
		{
			name:     "block 100000000 (well into tail emission)",
			blockNum: 100000000,
			expected: new(big.Int).Set(TailEmissionPerBlock),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalcBlockReward(new(big.Int).SetUint64(tt.blockNum))
			if got.Cmp(tt.expected) != 0 {
				t.Errorf("CalcBlockReward(%d) = %v, want %v", tt.blockNum, got, tt.expected)
			}
		})
	}
}

// TestCalcBlockRewardHalvingProgression verifies the halving sequence over multiple years.
func TestCalcBlockRewardHalvingProgression(t *testing.T) {
	flux := big.NewInt(params.Flux)
	initial := new(big.Int).Mul(big.NewInt(4708), flux)

	for era := uint64(0); era < 10; era++ {
		blockNum := era * params.BlocksPerYear
		if blockNum >= params.TailEmissionStartBlock {
			break
		}
		// Skip early bonus for era 0
		if era == 0 {
			blockNum = params.EarlyMinerBonusEndBlock
		}
		got := CalcBlockReward(new(big.Int).SetUint64(blockNum))
		expected := new(big.Int).Rsh(initial, uint(era))
		if got.Cmp(expected) != 0 {
			t.Errorf("era %d (block %d): got %v, want %v", era, blockNum, got, expected)
		}
	}
}

// helper to create a test statedb
func newTestStateDB() *state.StateDB {
	db := rawdb.NewMemoryDatabase()
	statedb, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)
	return statedb
}

// TestAccumulateRewardsYear1Split verifies the 70/10/10/10 split during year 1.
func TestAccumulateRewardsYear1Split(t *testing.T) {
	statedb := newTestStateDB()

	miner := common.HexToAddress("0x1111111111111111111111111111111111111111")
	dev := common.HexToAddress("0x2222222222222222222222222222222222222222")
	community := common.HexToAddress("0x3333333333333333333333333333333333333333")
	staker := common.HexToAddress("0x4444444444444444444444444444444444444444")

	config := &params.ChainConfig{
		ProgPow: &params.ProgpowConfig{
			DevFundAddress:       dev,
			CommunityFundAddress: community,
			StakerFundAddress:    staker,
		},
	}

	// Block in year 1, after early bonus (block 200000)
	header := &types.Header{
		Number:   big.NewInt(200000),
		Coinbase: miner,
	}

	accumulateRewards(config, statedb, header, nil)

	blockReward := CalcBlockReward(header.Number)

	expectedMiner := new(big.Int).Mul(blockReward, big.NewInt(70))
	expectedMiner.Div(expectedMiner, big.NewInt(100))

	expectedStaker := new(big.Int).Mul(blockReward, big.NewInt(10))
	expectedStaker.Div(expectedStaker, big.NewInt(100))

	expectedDev := new(big.Int).Mul(blockReward, big.NewInt(10))
	expectedDev.Div(expectedDev, big.NewInt(100))

	expectedCommunity := new(big.Int).Mul(blockReward, big.NewInt(10))
	expectedCommunity.Div(expectedCommunity, big.NewInt(100))

	if statedb.GetBalance(miner).Cmp(expectedMiner) != 0 {
		t.Errorf("miner balance = %v, want %v", statedb.GetBalance(miner), expectedMiner)
	}
	if statedb.GetBalance(staker).Cmp(expectedStaker) != 0 {
		t.Errorf("staker balance = %v, want %v", statedb.GetBalance(staker), expectedStaker)
	}
	if statedb.GetBalance(dev).Cmp(expectedDev) != 0 {
		t.Errorf("dev balance = %v, want %v", statedb.GetBalance(dev), expectedDev)
	}
	if statedb.GetBalance(community).Cmp(expectedCommunity) != 0 {
		t.Errorf("community balance = %v, want %v", statedb.GetBalance(community), expectedCommunity)
	}
}

// TestAccumulateRewardsPostYear1Split verifies the 75/15/10/0 split after year 1.
func TestAccumulateRewardsPostYear1Split(t *testing.T) {
	statedb := newTestStateDB()

	miner := common.HexToAddress("0x1111111111111111111111111111111111111111")
	dev := common.HexToAddress("0x2222222222222222222222222222222222222222")
	community := common.HexToAddress("0x3333333333333333333333333333333333333333")
	staker := common.HexToAddress("0x4444444444444444444444444444444444444444")

	config := &params.ChainConfig{
		ProgPow: &params.ProgpowConfig{
			DevFundAddress:       dev,
			CommunityFundAddress: community,
			StakerFundAddress:    staker,
		},
	}

	// Block in year 2 (after BlocksPerYear)
	blockNum := params.BlocksPerYear + 1000
	header := &types.Header{
		Number:   new(big.Int).SetUint64(blockNum),
		Coinbase: miner,
	}

	accumulateRewards(config, statedb, header, nil)

	blockReward := CalcBlockReward(header.Number)

	expectedMiner := new(big.Int).Mul(blockReward, big.NewInt(75))
	expectedMiner.Div(expectedMiner, big.NewInt(100))

	expectedStaker := new(big.Int).Mul(blockReward, big.NewInt(15))
	expectedStaker.Div(expectedStaker, big.NewInt(100))

	expectedDev := new(big.Int).Mul(blockReward, big.NewInt(10))
	expectedDev.Div(expectedDev, big.NewInt(100))

	if statedb.GetBalance(miner).Cmp(expectedMiner) != 0 {
		t.Errorf("miner balance = %v, want %v", statedb.GetBalance(miner), expectedMiner)
	}
	if statedb.GetBalance(staker).Cmp(expectedStaker) != 0 {
		t.Errorf("staker balance = %v, want %v", statedb.GetBalance(staker), expectedStaker)
	}
	if statedb.GetBalance(dev).Cmp(expectedDev) != 0 {
		t.Errorf("dev balance = %v, want %v", statedb.GetBalance(dev), expectedDev)
	}
	// Community should be 0 after year 1
	if statedb.GetBalance(community).Sign() != 0 {
		t.Errorf("community balance should be 0, got %v", statedb.GetBalance(community))
	}
}

// TestAccumulateRewardsEarlyBonus verifies the 2x multiplier for the first 150k blocks.
func TestAccumulateRewardsEarlyBonus(t *testing.T) {
	statedb := newTestStateDB()

	miner := common.HexToAddress("0x1111111111111111111111111111111111111111")
	dev := common.HexToAddress("0x2222222222222222222222222222222222222222")
	community := common.HexToAddress("0x3333333333333333333333333333333333333333")
	staker := common.HexToAddress("0x4444444444444444444444444444444444444444")

	config := &params.ChainConfig{
		ProgPow: &params.ProgpowConfig{
			DevFundAddress:       dev,
			CommunityFundAddress: community,
			StakerFundAddress:    staker,
		},
	}

	// Block 100 (early bonus)
	header := &types.Header{
		Number:   big.NewInt(100),
		Coinbase: miner,
	}

	accumulateRewards(config, statedb, header, nil)

	blockReward := CalcBlockReward(header.Number)
	// Verify the reward is 2x the initial
	expectedReward := new(big.Int).Mul(InitialBlockReward, big.NewInt(2))
	if blockReward.Cmp(expectedReward) != 0 {
		t.Errorf("early bonus reward = %v, want %v", blockReward, expectedReward)
	}

	// Verify miner gets 70% of the doubled reward
	expectedMiner := new(big.Int).Mul(blockReward, big.NewInt(70))
	expectedMiner.Div(expectedMiner, big.NewInt(100))
	if statedb.GetBalance(miner).Cmp(expectedMiner) != 0 {
		t.Errorf("miner balance = %v, want %v", statedb.GetBalance(miner), expectedMiner)
	}
}

// TestAccumulateRewardsTailEmission verifies fixed tail emission after block 42M.
func TestAccumulateRewardsTailEmission(t *testing.T) {
	statedb := newTestStateDB()

	miner := common.HexToAddress("0x1111111111111111111111111111111111111111")
	dev := common.HexToAddress("0x2222222222222222222222222222222222222222")
	community := common.HexToAddress("0x3333333333333333333333333333333333333333")
	staker := common.HexToAddress("0x4444444444444444444444444444444444444444")

	config := &params.ChainConfig{
		ProgPow: &params.ProgpowConfig{
			DevFundAddress:       dev,
			CommunityFundAddress: community,
			StakerFundAddress:    staker,
		},
	}

	// Block well into tail emission
	blockNum := params.TailEmissionStartBlock + 1000
	header := &types.Header{
		Number:   new(big.Int).SetUint64(blockNum),
		Coinbase: miner,
	}

	accumulateRewards(config, statedb, header, nil)

	// Verify tail emission uses post-year-1 split (75/15/10/0)
	expectedMiner := new(big.Int).Mul(TailEmissionPerBlock, big.NewInt(75))
	expectedMiner.Div(expectedMiner, big.NewInt(100))

	expectedStaker := new(big.Int).Mul(TailEmissionPerBlock, big.NewInt(15))
	expectedStaker.Div(expectedStaker, big.NewInt(100))

	expectedDev := new(big.Int).Mul(TailEmissionPerBlock, big.NewInt(10))
	expectedDev.Div(expectedDev, big.NewInt(100))

	if statedb.GetBalance(miner).Cmp(expectedMiner) != 0 {
		t.Errorf("miner balance = %v, want %v", statedb.GetBalance(miner), expectedMiner)
	}
	if statedb.GetBalance(staker).Cmp(expectedStaker) != 0 {
		t.Errorf("staker balance = %v, want %v", statedb.GetBalance(staker), expectedStaker)
	}
	if statedb.GetBalance(dev).Cmp(expectedDev) != 0 {
		t.Errorf("dev balance = %v, want %v", statedb.GetBalance(dev), expectedDev)
	}
	if statedb.GetBalance(community).Sign() != 0 {
		t.Errorf("community balance should be 0 in tail emission, got %v", statedb.GetBalance(community))
	}
}

// TestAccumulateRewardsWithUncles verifies uncle rewards are added on top of miner share.
func TestAccumulateRewardsWithUncles(t *testing.T) {
	statedb := newTestStateDB()

	miner := common.HexToAddress("0x1111111111111111111111111111111111111111")
	uncleMiner := common.HexToAddress("0x5555555555555555555555555555555555555555")
	dev := common.HexToAddress("0x2222222222222222222222222222222222222222")
	community := common.HexToAddress("0x3333333333333333333333333333333333333333")
	staker := common.HexToAddress("0x4444444444444444444444444444444444444444")

	config := &params.ChainConfig{
		ProgPow: &params.ProgpowConfig{
			DevFundAddress:       dev,
			CommunityFundAddress: community,
			StakerFundAddress:    staker,
		},
	}

	header := &types.Header{
		Number:   big.NewInt(200000),
		Coinbase: miner,
	}

	uncles := []*types.Header{
		{
			Number:   big.NewInt(199999),
			Coinbase: uncleMiner,
		},
	}

	accumulateRewards(config, statedb, header, uncles)

	blockReward := CalcBlockReward(header.Number)

	// Uncle miner reward: (199999 + 8 - 200000) * blockReward / 8 = 7/8 * blockReward
	expectedUncleReward := new(big.Int).Mul(big.NewInt(7), blockReward)
	expectedUncleReward.Div(expectedUncleReward, big.NewInt(8))

	if statedb.GetBalance(uncleMiner).Cmp(expectedUncleReward) != 0 {
		t.Errorf("uncle miner balance = %v, want %v", statedb.GetBalance(uncleMiner), expectedUncleReward)
	}

	// Miner gets: 70% of blockReward + blockReward/32 (uncle inclusion bonus)
	expectedMiner := new(big.Int).Mul(blockReward, big.NewInt(70))
	expectedMiner.Div(expectedMiner, big.NewInt(100))
	inclusionBonus := new(big.Int).Div(blockReward, big.NewInt(32))
	expectedMiner.Add(expectedMiner, inclusionBonus)

	if statedb.GetBalance(miner).Cmp(expectedMiner) != 0 {
		t.Errorf("miner balance = %v, want %v", statedb.GetBalance(miner), expectedMiner)
	}

	// Fund addresses should still get their shares (unaffected by uncles)
	expectedStaker := new(big.Int).Mul(blockReward, big.NewInt(10))
	expectedStaker.Div(expectedStaker, big.NewInt(100))
	if statedb.GetBalance(staker).Cmp(expectedStaker) != 0 {
		t.Errorf("staker balance = %v, want %v", statedb.GetBalance(staker), expectedStaker)
	}
}
