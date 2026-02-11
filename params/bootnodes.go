// Copyright 2015 The go-ethereum Authors
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

import "github.com/ethereum/go-ethereum/common"

// Yottaflux seed node DNS hostnames.
const (
	// YottafluxSeedDNS is the DNS hostname of the mainnet seed node.
	YottafluxSeedDNS = "seed.yottaflux.ai"

	// YottafluxTestnetSeedDNS is the DNS hostname of the testnet seed node.
	YottafluxTestnetSeedDNS = "seed-test.yottaflux.ai"
)

// MainnetBootnodes are the enode URLs of the P2P bootstrap nodes running on
// the Yottaflux mainnet.
//
// To populate, deploy the seed node at seed.yottaflux.ai, then run:
//
//	yottaflux --exec "admin.nodeInfo.enode" attach /path/to/yottaflux.ipc
//
// and paste the full enode:// URL here.
var MainnetBootnodes = []string{
	// "enode://<pubkey>@seed.yottaflux.ai:30403",
}

// YottafluxTestnetBootnodes are the enode URLs of the P2P bootstrap nodes
// running on the Yottaflux testnet.
//
// To populate, deploy the seed node at seed-test.yottaflux.ai, then run:
//
//	yottaflux --exec "admin.nodeInfo.enode" attach /path/to/yottaflux.ipc
//
// and paste the full enode:// URL here.
var YottafluxTestnetBootnodes = []string{
	// "enode://<pubkey>@seed-test.yottaflux.ai:30403",
}

// RopstenBootnodes are the enode URLs of the P2P bootstrap nodes running on the
// Ropsten test network.
var RopstenBootnodes = []string{}

// SepoliaBootnodes are the enode URLs of the P2P bootstrap nodes running on the
// Sepolia test network.
var SepoliaBootnodes = []string{}

// RinkebyBootnodes are the enode URLs of the P2P bootstrap nodes running on the
// Rinkeby test network.
var RinkebyBootnodes = []string{}

// GoerliBootnodes are the enode URLs of the P2P bootstrap nodes running on the
// Görli test network.
var GoerliBootnodes = []string{}

var KilnBootnodes = []string{}

var V5Bootnodes = []string{}

// KnownDNSNetwork returns the address of a public DNS-based node list for the given
// genesis hash and protocol.
func KnownDNSNetwork(genesis common.Hash, protocol string) string {
	return ""
}
