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
	"testing"
)

func TestProgpowDatasetSizes(t *testing.T) {
	for epoch := 0; epoch < maxCachedEpoch; epoch++ {
		expected := calcDatasetSize(epoch)
		actual := datasetSizes[epoch]
		if expected != actual {
			t.Errorf("dataset size mismatch for epoch %d: expected %d, got %d", epoch, expected, actual)
		}
	}
}

func TestProgpowCacheSizes(t *testing.T) {
	for epoch := 0; epoch < maxCachedEpoch; epoch++ {
		expected := calcCacheSize(epoch)
		actual := cacheSizes[epoch]
		if expected != actual {
			t.Errorf("cache size mismatch for epoch %d: expected %d, got %d", epoch, expected, actual)
		}
	}
}
