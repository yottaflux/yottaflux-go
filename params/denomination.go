// Copyright 2017 The go-ethereum Authors
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

// Modifications Copyright 2025 The Yottaflux Authors

package params

// Yottaflux denominations in zaps.
// These are the canonical Yottaflux denomination names.
// Example: To get the zap value of an amount in 'gigazap', use
//
//	new(big.Int).Mul(value, big.NewInt(params.GigaZap))
const (
	Zap     = 1
	GigaZap = 1e9
	Flux    = 1e18
)

// Backward-compatible aliases.
const (
	Wei   = Zap
	GWei  = GigaZap
	Ether = Flux
)
