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
	// Mainnet seed nodes not yet deployed.
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
	"enode://c7013c65beab0806791dc343dc4a9a127137316b0cc0c08bbc022fdb51f7e6210ca6209019fa64492581b5382d4817bf1df87c1716dcf99ea889fe5cc6b6dfb4@us-east-1a-seed-test.yottaflux.ai:30403",
	"enode://efe30d50875873b21bb9fadd64650c4264bbbd8ff76acf6e1498d82954386d4ba6614b0cc5f7707ed927f1d5240ad65b181f9bd1d7cdbaa55a5334b677ba8234@us-east-1b-seed-test.yottaflux.ai:30403",
	"enode://36470c6ee08fa2016e251aaebd1eddfd06a057a9b55defe7de0f582b8d8e7f7463b22b81ba488a68cd056cfde251892f1961015d62cfb170182ff16ca4f505fa@us-west-1a-seed-test.yottaflux.ai:30403",
	"enode://53a5c2235de5916c805b6b779cae15db9d5b416632f48be5c3373603f0294875d19de7d67019ef39a9b4795cb2d6409bcdbc5af43cc8f6e0ac54bc954f487822@us-west-1c-seed-test.yottaflux.ai:30403",
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
