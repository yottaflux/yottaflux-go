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

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// TestCalcDifficulty tests the Yottaflux difficulty adjustment algorithm.
// It uses the Byzantium-style adjustment WITHOUT the difficulty bomb.
// Formula: diff = parent_diff + (parent_diff / 2048 * max((2 if uncles else 1) - (timestamp - parent.timestamp) / 10, -99))
// The divisor of 10 targets 15-second block times.
func TestCalcDifficulty(t *testing.T) {
	parentDiff := big.NewInt(10000000) // 10M
	parentTime := uint64(1000000)
	parentNumber := big.NewInt(100)

	tests := []struct {
		name     string
		time     uint64
		uncles   bool
		expected *big.Int
	}{
		{
			// time_gap=1s, no uncles: adjustment = 1 - 0 = 1
			// diff = parent + parent/2048 * 1 = 10000000 + 4882 = 10004882
			name:     "difficulty increases (1s gap, no uncles)",
			time:     parentTime + 1,
			uncles:   false,
			expected: new(big.Int).Add(parentDiff, new(big.Int).Div(parentDiff, params.DifficultyBoundDivisor)),
		},
		{
			// time_gap=10s, no uncles: adjustment = 1 - 1 = 0
			// diff = parent + 0 = parent
			name:     "no change (10s gap, no uncles)",
			time:     parentTime + 10,
			uncles:   false,
			expected: new(big.Int).Set(parentDiff),
		},
		{
			// time_gap=20s, no uncles: adjustment = 1 - 2 = -1
			// diff = parent - parent/2048
			name:     "difficulty decreases (20s gap, no uncles)",
			time:     parentTime + 20,
			uncles:   false,
			expected: new(big.Int).Sub(parentDiff, new(big.Int).Div(parentDiff, params.DifficultyBoundDivisor)),
		},
		{
			// time_gap=1s, with uncles: adjustment = 2 - 0 = 2
			// diff = parent + parent/2048 * 2
			name:   "uncle bonus (1s gap, with uncles)",
			time:   parentTime + 1,
			uncles: true,
			expected: new(big.Int).Add(parentDiff,
				new(big.Int).Mul(big.NewInt(2), new(big.Int).Div(parentDiff, params.DifficultyBoundDivisor))),
		},
		{
			// time_gap=1000s, no uncles: adjustment = 1 - 100 = -99, clamped to -99
			// diff = parent + parent/2048 * (-99)
			// Let's compute: 10000000 - 99*4882 = 10000000 - 483318 = 9516682
			// That's above minimum, so it stays.
			name: "large gap clamps to -99 (1000s gap, no uncles)",
			time: parentTime + 1000,
			uncles: false,
			expected: func() *big.Int {
				adj := new(big.Int).Mul(big.NewInt(-99), new(big.Int).Div(parentDiff, params.DifficultyBoundDivisor))
				return new(big.Int).Add(parentDiff, adj)
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uncleHash := types.EmptyUncleHash
			if tt.uncles {
				// Use a non-empty uncle hash to indicate uncles present
				uncleHash = types.EmptyRootHash
			}

			parent := &types.Header{
				Number:     parentNumber,
				Time:       parentTime,
				Difficulty: new(big.Int).Set(parentDiff),
				UncleHash:  uncleHash,
			}

			got := CalcDifficulty(tt.time, parent)
			if got.Cmp(tt.expected) != 0 {
				t.Errorf("CalcDifficulty() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestCalcDifficultyMinimum tests that difficulty never drops below MinimumDifficulty.
func TestCalcDifficultyMinimum(t *testing.T) {
	parent := &types.Header{
		Number:     big.NewInt(100),
		Time:       1000000,
		Difficulty: new(big.Int).Set(params.MinimumDifficulty),
		UncleHash:  types.EmptyUncleHash,
	}

	// Very large time gap should clamp to minimum
	got := CalcDifficulty(parent.Time+10000, parent)
	if got.Cmp(params.MinimumDifficulty) != 0 {
		t.Errorf("expected MinimumDifficulty=%v, got %v", params.MinimumDifficulty, got)
	}
}

// TestCalcDifficultyNoBomb verifies there is NO difficulty bomb at high block numbers.
// This is the key difference from ethash: the bomb term is absent.
func TestCalcDifficultyNoBomb(t *testing.T) {
	parentDiff := big.NewInt(10000000)

	// Test at block 20,000,000 with 10s gap (should produce same diff as parent)
	parent := &types.Header{
		Number:     big.NewInt(20000000),
		Time:       1000000,
		Difficulty: new(big.Int).Set(parentDiff),
		UncleHash:  types.EmptyUncleHash,
	}

	got := CalcDifficulty(parent.Time+10, parent)
	// With 10s gap and no uncles, adjustment = 0, so diff = parent_diff
	// If there were a bomb, it would add 2^((20000000/100000)-2) which is huge
	if got.Cmp(parentDiff) != 0 {
		t.Errorf("expected no bomb at block 20M: got %v, want %v", got, parentDiff)
	}
}

// TestCalcDifficultyBounded tests the bounded wrapper.
func TestCalcDifficultyBounded(t *testing.T) {
	parent := &types.Header{
		Number:     big.NewInt(100),
		Time:       1000000,
		Difficulty: big.NewInt(10000000),
		UncleHash:  types.EmptyUncleHash,
	}

	got := CalcDifficultyBounded(parent.Time+10, parent)
	if got.Cmp(params.MinimumDifficulty) < 0 {
		t.Errorf("bounded diff below minimum: %v", got)
	}
}
