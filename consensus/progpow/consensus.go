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

package progpow

import (
	"bytes"
	"errors"
	"fmt"
	"math/big"
	"runtime"
	"time"

	mapset "github.com/deckarep/golang-set"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	"golang.org/x/crypto/sha3"
)

// ProgPow proof-of-work protocol constants.
var (
	// InitialBlockReward is 4708 YTX in zaps (the base unit).
	InitialBlockReward = new(big.Int).Mul(big.NewInt(4708), big.NewInt(params.Flux))

	// TailEmissionPerBlock is the fixed per-block reward after 20 years.
	// Total annual tail emission = 105,000,000 YTX; per-block = 105,000,000 / BlocksPerYear ≈ 49.93 YTX.
	TailEmissionPerBlock = new(big.Int).Div(
		new(big.Int).Mul(big.NewInt(105_000_000), big.NewInt(params.Flux)),
		new(big.Int).SetUint64(params.BlocksPerYear),
	)

	// Reward split percentages (numerator out of 100).
	// Year 1: 70% miner, 10% staker, 10% dev, 10% community.
	minerPctYear1     = big.NewInt(70)
	stakerPctYear1    = big.NewInt(10)
	devPctYear1       = big.NewInt(10)
	communityPctYear1 = big.NewInt(10)
	// Post-year 1: 75% miner, 15% staker, 10% dev, 0% community.
	minerPctPostYear1  = big.NewInt(75)
	stakerPctPostYear1 = big.NewInt(15)
	devPctPostYear1    = big.NewInt(10)

	big100 = big.NewInt(100)

	// BlockReward is kept as an alias for backward compatibility in tests.
	// It now returns the initial block reward (4708 YTX).
	BlockReward = InitialBlockReward

	maxUncles                     = 2     // Maximum number of uncles allowed in a single block
	allowedFutureBlockTimeSeconds = int64(15) // Max seconds from current time allowed for blocks, before they're considered future blocks
)

// Various error messages to mark blocks invalid. These should be private to
// prevent engine specific errors from being referenced in the remainder of the
// codebase, inherently breaking if the engine is swapped out. Please put common
// error types into the consensus package.
var (
	errOlderBlockTime    = errors.New("timestamp older than parent")
	errTooManyUncles     = errors.New("too many uncles")
	errDuplicateUncle    = errors.New("duplicate uncle")
	errUncleIsAncestor   = errors.New("uncle is ancestor")
	errDanglingUncle     = errors.New("uncle's parent is not ancestor")
	errInvalidDifficulty = errors.New("non-positive difficulty")
	errInvalidMixDigest  = errors.New("invalid mix digest")
	errInvalidPoW        = errors.New("invalid proof-of-work")
)

// Author implements consensus.Engine, returning the header's coinbase as the
// proof-of-work verified author of the block.
func (progpow *Progpow) Author(header *types.Header) (common.Address, error) {
	return header.Coinbase, nil
}

// VerifyHeader checks whether a header conforms to the consensus rules of the
// stock Yottaflux progpow engine.
func (progpow *Progpow) VerifyHeader(chain consensus.ChainHeaderReader, header *types.Header, seal bool) error {
	// If we're running a full engine faking, accept any input as valid
	if progpow.config.PowMode == ModeFullFake {
		return nil
	}
	// Short circuit if the header is known, or its parent not
	number := header.Number.Uint64()
	if chain.GetHeader(header.Hash(), number) != nil {
		return nil
	}
	parent := chain.GetHeader(header.ParentHash, number-1)
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}
	// Sanity checks passed, do a proper verification
	return progpow.verifyHeader(chain, header, parent, false, seal, time.Now().Unix())
}

// VerifyHeaders is similar to VerifyHeader, but verifies a batch of headers
// concurrently. The method returns a quit channel to abort the operations and
// a results channel to retrieve the async verifications.
func (progpow *Progpow) VerifyHeaders(chain consensus.ChainHeaderReader, headers []*types.Header, seals []bool) (chan<- struct{}, <-chan error) {
	// If we're running a full engine faking, accept any input as valid
	if progpow.config.PowMode == ModeFullFake || len(headers) == 0 {
		abort, results := make(chan struct{}), make(chan error, len(headers))
		for i := 0; i < len(headers); i++ {
			results <- nil
		}
		return abort, results
	}

	// Spawn as many workers as allowed threads
	workers := runtime.GOMAXPROCS(0)
	if len(headers) < workers {
		workers = len(headers)
	}

	// Create a task channel and spawn the verifiers
	var (
		inputs  = make(chan int)
		done    = make(chan int, workers)
		errors  = make([]error, len(headers))
		abort   = make(chan struct{})
		unixNow = time.Now().Unix()
	)
	for i := 0; i < workers; i++ {
		go func() {
			for index := range inputs {
				errors[index] = progpow.verifyHeaderWorker(chain, headers, seals, index, unixNow)
				done <- index
			}
		}()
	}

	errorsOut := make(chan error, len(headers))
	go func() {
		defer close(inputs)
		var (
			in, out = 0, 0
			checked = make([]bool, len(headers))
			inputs  = inputs
		)
		for {
			select {
			case inputs <- in:
				if in++; in == len(headers) {
					// Reached end of headers. Stop sending to workers.
					inputs = nil
				}
			case index := <-done:
				for checked[index] = true; checked[out]; out++ {
					errorsOut <- errors[out]
					if out == len(headers)-1 {
						return
					}
				}
			case <-abort:
				return
			}
		}
	}()
	return abort, errorsOut
}

func (progpow *Progpow) verifyHeaderWorker(chain consensus.ChainHeaderReader, headers []*types.Header, seals []bool, index int, unixNow int64) error {
	var parent *types.Header
	if index == 0 {
		parent = chain.GetHeader(headers[0].ParentHash, headers[0].Number.Uint64()-1)
	} else if headers[index-1].Hash() == headers[index].ParentHash {
		parent = headers[index-1]
	}
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}
	return progpow.verifyHeader(chain, headers[index], parent, false, seals[index], unixNow)
}

// VerifyUncles verifies that the given block's uncles conform to the consensus
// rules of the stock Yottaflux progpow engine.
func (progpow *Progpow) VerifyUncles(chain consensus.ChainReader, block *types.Block) error {
	// If we're running a full engine faking, accept any input as valid
	if progpow.config.PowMode == ModeFullFake {
		return nil
	}
	// Verify that there are at most 2 uncles included in this block
	if len(block.Uncles()) > maxUncles {
		return errTooManyUncles
	}
	if len(block.Uncles()) == 0 {
		return nil
	}
	// Gather the set of past uncles and ancestors
	uncles, ancestors := mapset.NewSet(), make(map[common.Hash]*types.Header)

	number, parent := block.NumberU64()-1, block.ParentHash()
	for i := 0; i < 7; i++ {
		ancestorHeader := chain.GetHeader(parent, number)
		if ancestorHeader == nil {
			break
		}
		ancestors[parent] = ancestorHeader
		// If the ancestor doesn't have any uncles, we don't have to iterate them
		if ancestorHeader.UncleHash != types.EmptyUncleHash {
			// Need to add those uncles to the banned list too
			ancestor := chain.GetBlock(parent, number)
			if ancestor == nil {
				break
			}
			for _, uncle := range ancestor.Uncles() {
				uncles.Add(uncle.Hash())
			}
		}
		parent, number = ancestorHeader.ParentHash, number-1
	}
	ancestors[block.Hash()] = block.Header()
	uncles.Add(block.Hash())

	// Verify each of the uncles that it's recent, but not an ancestor
	for _, uncle := range block.Uncles() {
		// Make sure every uncle is rewarded only once
		hash := uncle.Hash()
		if uncles.Contains(hash) {
			return errDuplicateUncle
		}
		uncles.Add(hash)

		// Make sure the uncle has a valid ancestry
		if ancestors[hash] != nil {
			return errUncleIsAncestor
		}
		if ancestors[uncle.ParentHash] == nil || uncle.ParentHash == block.ParentHash() {
			return errDanglingUncle
		}
		if err := progpow.verifyHeader(chain, uncle, ancestors[uncle.ParentHash], true, true, time.Now().Unix()); err != nil {
			return err
		}
	}
	return nil
}

// verifyHeader checks whether a header conforms to the consensus rules of the
// stock Yottaflux progpow engine.
func (progpow *Progpow) verifyHeader(chain consensus.ChainHeaderReader, header, parent *types.Header, uncle bool, seal bool, unixNow int64) error {
	// Ensure that the header's extra-data section is of a reasonable size
	if uint64(len(header.Extra)) > params.MaximumExtraDataSize {
		return fmt.Errorf("extra-data too long: %d > %d", len(header.Extra), params.MaximumExtraDataSize)
	}
	// Verify the header's timestamp
	if !uncle {
		if header.Time > uint64(unixNow+allowedFutureBlockTimeSeconds) {
			return consensus.ErrFutureBlock
		}
	}
	if header.Time <= parent.Time {
		return errOlderBlockTime
	}
	// Verify the block's difficulty based on its timestamp and parent's difficulty
	expected := progpow.CalcDifficulty(chain, header.Time, parent)

	if expected.Cmp(header.Difficulty) != 0 {
		return fmt.Errorf("invalid difficulty: have %v, want %v", header.Difficulty, expected)
	}
	// Verify that the gas limit is <= 2^63-1
	if header.GasLimit > params.MaxGasLimit {
		return fmt.Errorf("invalid gasLimit: have %v, max %v", header.GasLimit, params.MaxGasLimit)
	}
	// Verify that the gasUsed is <= gasLimit
	if header.GasUsed > header.GasLimit {
		return fmt.Errorf("invalid gasUsed: have %d, gasLimit %d", header.GasUsed, header.GasLimit)
	}
	// Verify the block's gas usage and (if applicable) verify the base fee.
	if !chain.Config().IsLondon(header.Number) {
		// Verify BaseFee not present before EIP-1559 fork.
		if header.BaseFee != nil {
			return fmt.Errorf("invalid baseFee before fork: have %d, expected 'nil'", header.BaseFee)
		}
		if err := misc.VerifyGaslimit(parent.GasLimit, header.GasLimit); err != nil {
			return err
		}
	} else if err := misc.VerifyEip1559Header(chain.Config(), parent, header); err != nil {
		// Verify the header's EIP-1559 attributes.
		return err
	}
	// Verify that the block number is parent's +1
	if diff := new(big.Int).Sub(header.Number, parent.Number); diff.Cmp(big.NewInt(1)) != 0 {
		return consensus.ErrInvalidNumber
	}
	// Verify the engine specific seal securing the block
	if seal {
		if err := progpow.verifySeal(chain, header, false); err != nil {
			return err
		}
	}
	return nil
}

// CalcDifficulty is the difficulty adjustment algorithm. It returns
// the difficulty that a new block should have when created at time
// given the parent block's time and difficulty.
func (progpow *Progpow) CalcDifficulty(chain consensus.ChainHeaderReader, time uint64, parent *types.Header) *big.Int {
	return CalcDifficulty(time, parent)
}

// Some weird constants to avoid constant memory allocs for them.
var (
	big1       = big.NewInt(1)
	big2       = big.NewInt(2)
	big10      = big.NewInt(10)
	bigMinus99 = big.NewInt(-99)
)

// CalcDifficulty is the Yottaflux difficulty adjustment algorithm.
// It uses the Byzantium-style adjustment WITHOUT the difficulty bomb.
// diff = parent_diff + (parent_diff / 2048 * max((2 if uncles else 1) - (timestamp - parent.timestamp) / 10, -99))
// The divisor of 10 targets 15-second block times.
func CalcDifficulty(time uint64, parent *types.Header) *big.Int {
	bigTime := new(big.Int).SetUint64(time)
	bigParentTime := new(big.Int).SetUint64(parent.Time)

	// holds intermediate values to make the algo easier to read & audit
	x := new(big.Int)
	y := new(big.Int)

	// (2 if len(parent_uncles) else 1) - (block_timestamp - parent_timestamp) // 10
	x.Sub(bigTime, bigParentTime)
	x.Div(x, big10)
	if parent.UncleHash == types.EmptyUncleHash {
		x.Sub(big1, x)
	} else {
		x.Sub(big2, x)
	}
	// max((2 if len(parent_uncles) else 1) - (block_timestamp - parent_timestamp) // 10, -99)
	if x.Cmp(bigMinus99) < 0 {
		x.Set(bigMinus99)
	}
	// parent_diff + (parent_diff / 2048 * max((2 if len(parent.uncles) else 1) - ((timestamp - parent.timestamp) // 10), -99))
	y.Div(parent.Difficulty, params.DifficultyBoundDivisor)
	x.Mul(y, x)
	x.Add(parent.Difficulty, x)

	// minimum difficulty can ever be (before exponential factor)
	if x.Cmp(params.MinimumDifficulty) < 0 {
		x.Set(params.MinimumDifficulty)
	}
	// NO difficulty bomb - this is the key difference from ethash
	return x
}

// verifySeal checks whether a block satisfies the PoW difficulty requirements,
// using ProgPow verification.
func (progpow *Progpow) verifySeal(chain consensus.ChainHeaderReader, header *types.Header, fulldag bool) error {
	// If we're running a fake PoW, accept any seal as valid
	if progpow.config.PowMode == ModeFake || progpow.config.PowMode == ModeFullFake {
		time.Sleep(progpow.fakeDelay)
		if progpow.fakeFail == header.Number.Uint64() {
			return errInvalidPoW
		}
		return nil
	}
	// If we're running a shared PoW, delegate verification to it
	if progpow.shared != nil {
		return progpow.shared.verifySeal(chain, header, fulldag)
	}
	// Ensure that we have a valid difficulty for the block
	if header.Difficulty.Sign() <= 0 {
		return errInvalidDifficulty
	}
	// Recompute the digest and PoW values using ProgPow
	number := header.Number.Uint64()

	var (
		digest []byte
		result []byte
	)
	// If fast-but-heavy PoW verification was requested, use a full dataset
	if fulldag {
		dataset := progpow.dataset(number, true)
		if dataset.generated() {
			digest, result = progpowFull(dataset.dataset, progpow.SealHash(header).Bytes(), header.Nonce.Uint64(), number)

			// Datasets are unmapped in a finalizer. Ensure that the dataset stays alive
			// until after the call to progpowFull so it's not unmapped while being used.
			runtime.KeepAlive(dataset)
		} else {
			// Dataset not yet generated, don't hang, use a cache instead
			fulldag = false
		}
	}
	// If slow-but-light PoW verification was requested (or DAG not yet ready), use a cache
	if !fulldag {
		c := progpow.cache(number)

		size := datasetSize(number)
		if progpow.config.PowMode == ModeTest {
			size = 32 * 1024
		}
		if c.cDag == nil {
			cDag := make([]uint32, progpowCacheWords)
			generateCDag(cDag, c.cache, number/epochLength)
			c.cDag = cDag
		}
		digest, result = progpowLight(size, c.cache, progpow.SealHash(header).Bytes(), header.Nonce.Uint64(), number, c.cDag)

		// Caches are unmapped in a finalizer. Ensure that the cache stays alive
		// until after the call to progpowLight so it's not unmapped while being used.
		runtime.KeepAlive(c)
	}
	// Verify the calculated values against the ones provided in the header
	if !bytes.Equal(header.MixDigest[:], digest) {
		return errInvalidMixDigest
	}
	target := new(big.Int).Div(two256, header.Difficulty)
	if new(big.Int).SetBytes(result).Cmp(target) > 0 {
		return errInvalidPoW
	}
	return nil
}

// Prepare implements consensus.Engine, initializing the difficulty field of a
// header to conform to the progpow protocol. The changes are done inline.
func (progpow *Progpow) Prepare(chain consensus.ChainHeaderReader, header *types.Header) error {
	parent := chain.GetHeader(header.ParentHash, header.Number.Uint64()-1)
	if parent == nil {
		return consensus.ErrUnknownAncestor
	}
	header.Difficulty = progpow.CalcDifficulty(chain, header.Time, parent)
	return nil
}

// Finalize implements consensus.Engine, accumulating the block and uncle rewards,
// setting the final state on the header
func (progpow *Progpow) Finalize(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB, txs []*types.Transaction, uncles []*types.Header) {
	// Accumulate any block and uncle rewards and commit the final state root
	accumulateRewards(chain.Config(), state, header, uncles)
	header.Root = state.IntermediateRoot(chain.Config().IsEIP158(header.Number))
}

// FinalizeAndAssemble implements consensus.Engine, accumulating the block and
// uncle rewards, setting the final state and assembling the block.
func (progpow *Progpow) FinalizeAndAssemble(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB, txs []*types.Transaction, uncles []*types.Header, receipts []*types.Receipt) (*types.Block, error) {
	// Finalize block
	progpow.Finalize(chain, header, state, txs, uncles)

	// Header seems complete, assemble into a block and return
	return types.NewBlock(header, txs, uncles, receipts, trie.NewStackTrie(nil)), nil
}

// SealHash returns the hash of a block prior to it being sealed.
func (progpow *Progpow) SealHash(header *types.Header) (hash common.Hash) {
	hasher := sha3.NewLegacyKeccak256()

	enc := []interface{}{
		header.ParentHash,
		header.UncleHash,
		header.Coinbase,
		header.Root,
		header.TxHash,
		header.ReceiptHash,
		header.Bloom,
		header.Difficulty,
		header.Number,
		header.GasLimit,
		header.GasUsed,
		header.Time,
		header.Extra,
	}
	if header.BaseFee != nil {
		enc = append(enc, header.BaseFee)
	}
	rlp.Encode(hasher, enc)
	hasher.Sum(hash[:0])
	return hash
}

// Some weird constants to avoid constant memory allocs for them.
var (
	big8  = big.NewInt(8)
	big32 = big.NewInt(32)
)

// CalcBlockReward computes the total block reward for a given block number,
// implementing the Yottaflux halving schedule, early miner bonus, and tail emission.
func CalcBlockReward(blockNumber *big.Int) *big.Int {
	blockNum := blockNumber.Uint64()

	// Tail emission: fixed ~49.93 YTX/block after 20 years
	if blockNum >= params.TailEmissionStartBlock {
		return new(big.Int).Set(TailEmissionPerBlock)
	}

	// Compute era (year) for halving: era = blockNum / BlocksPerYear
	era := blockNum / params.BlocksPerYear

	// Start with initial reward, right-shift by era (halving each year)
	reward := new(big.Int).Set(InitialBlockReward)
	if era > 0 {
		reward.Rsh(reward, uint(era))
	}

	// Early miner 2x bonus for the first 150,000 blocks
	if blockNum < params.EarlyMinerBonusEndBlock {
		reward.Mul(reward, big2)
	}

	return reward
}

// accumulateRewards credits the coinbase of the given block with the mining
// reward split among miner, dev fund, staker fund, and community fund.
// Uncle miners also receive rewards based on the full block reward.
func accumulateRewards(config *params.ChainConfig, state *state.StateDB, header *types.Header, uncles []*types.Header) {
	blockReward := CalcBlockReward(header.Number)

	// Determine reward split percentages based on year
	blockNum := header.Number.Uint64()
	isYear1 := blockNum < params.BlocksPerYear

	var minerPct, stakerPct, devPct, communityPct *big.Int
	if isYear1 {
		minerPct = minerPctYear1
		stakerPct = stakerPctYear1
		devPct = devPctYear1
		communityPct = communityPctYear1
	} else {
		minerPct = minerPctPostYear1
		stakerPct = stakerPctPostYear1
		devPct = devPctPostYear1
		communityPct = big.NewInt(0)
	}

	// Compute fund shares
	stakerShare := new(big.Int).Mul(blockReward, stakerPct)
	stakerShare.Div(stakerShare, big100)

	devShare := new(big.Int).Mul(blockReward, devPct)
	devShare.Div(devShare, big100)

	communityShare := new(big.Int).Mul(blockReward, communityPct)
	communityShare.Div(communityShare, big100)

	// Miner share = blockReward * minerPct / 100
	minerShare := new(big.Int).Mul(blockReward, minerPct)
	minerShare.Div(minerShare, big100)

	// Add uncle rewards on top of miner share
	r := new(big.Int)
	for _, uncle := range uncles {
		// Uncle miner reward: (uncle.Number + 8 - header.Number) * blockReward / 8
		r.Add(uncle.Number, big8)
		r.Sub(r, header.Number)
		r.Mul(r, blockReward)
		r.Div(r, big8)
		state.AddBalance(uncle.Coinbase, r)

		// Miner inclusion reward: blockReward / 32
		r.Div(blockReward, big32)
		minerShare.Add(minerShare, r)
	}

	// Credit miner
	state.AddBalance(header.Coinbase, minerShare)

	// Credit fund addresses (only if ProgPow config exists)
	if config.ProgPow != nil {
		if stakerShare.Sign() > 0 {
			state.AddBalance(config.ProgPow.StakerFundAddress, stakerShare)
		}
		if devShare.Sign() > 0 {
			state.AddBalance(config.ProgPow.DevFundAddress, devShare)
		}
		if communityShare.Sign() > 0 {
			state.AddBalance(config.ProgPow.CommunityFundAddress, communityShare)
		}
	}
}

// Exported for fuzzing
var DifficultyCalculator = CalcDifficulty

// CalcDifficultyBounded performs the Yottaflux difficulty calculation with bounds checking.
func CalcDifficultyBounded(time uint64, parent *types.Header) *big.Int {
	diff := CalcDifficulty(time, parent)
	if diff.Cmp(params.MinimumDifficulty) < 0 {
		return new(big.Int).Set(params.MinimumDifficulty)
	}
	return math.BigMin(diff, math.MaxBig256)
}
