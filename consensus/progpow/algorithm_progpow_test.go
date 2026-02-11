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
	"bytes"
	"testing"
)

// TestKeccakF800Short tests the keccak-f[800] short output (uint64) used in ProgPow
// for generating the initial seed from headerHash, nonce, and result.
func TestKeccakF800Short(t *testing.T) {
	tests := []struct {
		name       string
		headerHash []byte
		nonce      uint64
		result     []uint32
	}{
		{
			name:       "zero inputs",
			headerHash: make([]byte, 32),
			nonce:      0,
			result:     make([]uint32, 8),
		},
		{
			name: "non-zero inputs",
			headerHash: []byte{
				0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
				0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
				0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
				0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
			},
			nonce:  0x123456789abcdef0,
			result: []uint32{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88},
		},
	}

	// Run each test twice: first to capture the reference output, then to verify determinism.
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out1 := keccakF800Short(tt.headerHash, tt.nonce, tt.result)
			out2 := keccakF800Short(tt.headerHash, tt.nonce, tt.result)
			if out1 != out2 {
				t.Fatalf("keccakF800Short not deterministic: got %x then %x", out1, out2)
			}
			t.Logf("keccakF800Short(%q, 0x%x) = 0x%016x", tt.name, tt.nonce, out1)
		})
	}
}

// TestKeccakF800Long tests the keccak-f[800] long output (32 bytes) used in ProgPow
// for generating the final hash from headerHash, seed, and result.
func TestKeccakF800Long(t *testing.T) {
	tests := []struct {
		name       string
		headerHash []byte
		nonce      uint64
		result     []uint32
	}{
		{
			name:       "zero inputs",
			headerHash: make([]byte, 32),
			nonce:      0,
			result:     make([]uint32, 8),
		},
		{
			name: "non-zero inputs",
			headerHash: []byte{
				0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
				0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
				0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18,
				0x19, 0x1a, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f, 0x20,
			},
			nonce:  0x123456789abcdef0,
			result: []uint32{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out1 := keccakF800Long(tt.headerHash, tt.nonce, tt.result)
			out2 := keccakF800Long(tt.headerHash, tt.nonce, tt.result)
			if !bytes.Equal(out1, out2) {
				t.Fatalf("keccakF800Long not deterministic: got %x then %x", out1, out2)
			}
			if len(out1) != 32 {
				t.Fatalf("expected 32-byte output, got %d bytes", len(out1))
			}
			t.Logf("keccakF800Long(%q, 0x%x) = %x", tt.name, tt.nonce, out1)
		})
	}
}

// TestKiss99 tests the KISS99 PRNG using the spec's initial constants.
// We run 100 iterations and verify the output is deterministic.
func TestKiss99(t *testing.T) {
	st := kiss99State{
		z:     362436069,
		w:     521288629,
		jsr:   123456789,
		jcong: 380116160,
	}

	// Run 100 iterations, capturing the last value.
	var val uint32
	for i := 0; i < 100; i++ {
		val = kiss99(&st)
	}
	t.Logf("kiss99 after 100 iterations: %d (0x%08x)", val, val)

	// Run again to verify determinism.
	st2 := kiss99State{
		z:     362436069,
		w:     521288629,
		jsr:   123456789,
		jcong: 380116160,
	}
	var val2 uint32
	for i := 0; i < 100; i++ {
		val2 = kiss99(&st2)
	}
	if val != val2 {
		t.Fatalf("kiss99 not deterministic: %d != %d", val, val2)
	}
}

// TestFillMix tests the mix register initialization function.
func TestFillMix(t *testing.T) {
	tests := []struct {
		name   string
		seed   uint64
		laneId uint32
	}{
		{"seed=0,lane=0", 0, 0},
		{"seed=12345,lane=7", 12345, 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mix1 := fillMix(tt.seed, tt.laneId)
			mix2 := fillMix(tt.seed, tt.laneId)
			if mix1 != mix2 {
				t.Fatal("fillMix not deterministic")
			}
			// Verify all 32 registers are populated (not all zero for non-trivial inputs)
			if tt.seed != 0 || tt.laneId != 0 {
				allZero := true
				for _, v := range mix1 {
					if v != 0 {
						allZero = false
						break
					}
				}
				if allZero {
					t.Fatal("fillMix returned all zeros for non-trivial input")
				}
			}
			t.Logf("fillMix(seed=%d, lane=%d): first 4 regs = [%08x, %08x, %08x, %08x]",
				tt.seed, tt.laneId, mix1[0], mix1[1], mix1[2], mix1[3])
		})
	}
}

// TestProgpowMath tests all 11 math operations (r%11 = 0..10).
func TestProgpowMath(t *testing.T) {
	tests := []struct {
		a, b, r  uint32
		expected uint32
		op       string
	}{
		// r%11 == 0: a + b
		{10, 20, 0, 30, "add"},
		// r%11 == 1: a * b
		{5, 7, 1, 35, "mul"},
		// r%11 == 2: higher32(uint64(a) * uint64(b))
		// 0x80000000 * 4 = 0x200000000, higher32 = 2
		{0x80000000, 4, 2, 2, "mulhi"},
		// r%11 == 3: min(a, b)
		{100, 50, 3, 50, "min(a<b)"},
		{30, 80, 3, 30, "min(a>b)"},
		// r%11 == 4: rotl32(a, b)
		{1, 1, 4, 2, "rotl"},
		// r%11 == 5: rotr32(a, b)
		{2, 1, 5, 1, "rotr"},
		// r%11 == 6: a & b
		{0xFF00, 0x0FF0, 6, 0x0F00, "and"},
		// r%11 == 7: a | b
		{0xFF00, 0x0FF0, 7, 0xFFF0, "or"},
		// r%11 == 8: a ^ b
		{0xFF00, 0x0FF0, 8, 0xF0F0, "xor"},
		// r%11 == 9: clz(a) + clz(b)
		{1, 1, 9, 62, "clz"},
		// r%11 == 10: popcount(a) + popcount(b)
		{0xFF, 0x0F, 10, 12, "popcount"},
	}

	for _, tt := range tests {
		t.Run(tt.op, func(t *testing.T) {
			got := progpowMath(tt.a, tt.b, tt.r)
			if got != tt.expected {
				t.Errorf("progpowMath(%d, %d, %d) = %d, want %d", tt.a, tt.b, tt.r, got, tt.expected)
			}
		})
	}
}

// TestProgpowLight tests the light verification path of ProgPow.
// It generates a small epoch-0 cache and cDag, then computes a hash.
func TestProgpowLight(t *testing.T) {
	// Use a test-mode cache size (1024 bytes = 256 uint32s)
	const testCacheSize = 1024
	cache := make([]uint32, testCacheSize/4)
	seed := seedHash(0) // epoch 0
	generateCache(cache, 0, seed)

	// Generate cDag
	cDag := make([]uint32, progpowCacheWords)
	generateCDag(cDag, cache, 0)

	// Use a test-mode dataset size
	const testDatasetSize = 32 * 1024
	hash := make([]byte, 32)

	digest, result := progpowLight(testDatasetSize, cache, hash, 0, 0, cDag)

	if len(digest) == 0 || len(result) == 0 {
		t.Fatal("progpowLight returned empty outputs")
	}

	// Verify determinism
	digest2, result2 := progpowLight(testDatasetSize, cache, hash, 0, 0, cDag)
	if !bytes.Equal(digest, digest2) || !bytes.Equal(result, result2) {
		t.Fatal("progpowLight not deterministic")
	}

	t.Logf("progpowLight digest: %x", digest)
	t.Logf("progpowLight result: %x", result)
}

// TestProgpowLightDifferentNonces verifies that different nonces produce different outputs.
func TestProgpowLightDifferentNonces(t *testing.T) {
	const testCacheSize = 1024
	cache := make([]uint32, testCacheSize/4)
	seed := seedHash(0)
	generateCache(cache, 0, seed)

	cDag := make([]uint32, progpowCacheWords)
	generateCDag(cDag, cache, 0)

	const testDatasetSize = 32 * 1024
	hash := make([]byte, 32)

	digest0, result0 := progpowLight(testDatasetSize, cache, hash, 0, 0, cDag)
	digest1, result1 := progpowLight(testDatasetSize, cache, hash, 1, 0, cDag)

	if bytes.Equal(digest0, digest1) {
		t.Error("different nonces produced same digest")
	}
	if bytes.Equal(result0, result1) {
		t.Error("different nonces produced same result")
	}
}

// TestProgpowLightDifferentBlocks verifies that different block numbers
// (which change the ProgPow period) produce different outputs.
func TestProgpowLightDifferentBlocks(t *testing.T) {
	const testCacheSize = 1024
	cache := make([]uint32, testCacheSize/4)
	seed := seedHash(0)
	generateCache(cache, 0, seed)

	cDag := make([]uint32, progpowCacheWords)
	generateCDag(cDag, cache, 0)

	const testDatasetSize = 32 * 1024
	hash := make([]byte, 32)

	// Block 0 (period 0) vs block 10 (period 1, since progpowPeriodLength=10)
	digest0, result0 := progpowLight(testDatasetSize, cache, hash, 0, 0, cDag)
	digest10, result10 := progpowLight(testDatasetSize, cache, hash, 0, 10, cDag)

	if bytes.Equal(digest0, digest10) {
		t.Error("different block numbers produced same digest")
	}
	if bytes.Equal(result0, result10) {
		t.Error("different block numbers produced same result")
	}
}
