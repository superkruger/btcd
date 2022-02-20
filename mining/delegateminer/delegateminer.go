// Copyright (c) 2014-2016 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package delegateminer

import (
	"errors"
	"fmt"
	"github.com/btcsuite/btcd/socketserver"
	"math/rand"
	"os"
	"sync"
	"time"

	"github.com/btcsuite/btcd/blockchain"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/mining"
	"github.com/btcsuite/btcd/wire"
)

const (
	// maxNonce is the maximum value a nonce can be in a block header.
	maxNonce = ^uint32(0) // 2^32 - 1

	// maxExtraNonce is the maximum value an extra nonce used in a coinbase
	// transaction can be.
	maxExtraNonce = ^uint64(0) // 2^64 - 1
)

// Config is a descriptor containing the cpu miner configuration.
type Config struct {
	// ChainParams identifies which chain parameters the cpu miner is
	// associated with.
	ChainParams *chaincfg.Params

	// BlockTemplateGenerator identifies the instance to use in order to
	// generate block templates that the miner will attempt to solve.
	BlockTemplateGenerator *mining.BlkTmplGenerator

	// MiningAddrs is a list of payment addresses to use for the generated
	// blocks.  Each generated block will randomly choose one of them.
	MiningAddrs []btcutil.Address

	// DelegateAddrs is a list of payment addresses to use for the generated
	// blocks in the delegate miner.
	// Each generated block will randomly choose one of them.
	DelegateAddrs []btcutil.Address

	// ProcessBlock defines the function to call with any solved blocks.
	// It typically must run the provided block through the same set of
	// rules and handling as any other block coming from the network.
	ProcessBlock func(*btcutil.Block, blockchain.BehaviorFlags) (bool, error)

	// ConnectedCount defines the function to use to obtain how many other
	// peers the server is connected to.  This is used by the automatic
	// persistent mining routine to determine whether or it should attempt
	// mining.  This is useful because there is no point in mining when not
	// connected to any peers since there would no be anyone to send any
	// found blocks to.
	ConnectedCount func() int32

	// IsCurrent defines the function to use to obtain whether or not the
	// block chain is current.  This is used by the automatic persistent
	// mining routine to determine whether or it should attempt mining.
	// This is useful because there is no point in mining if the chain is
	// not current since any solved blocks would be on a side chain and and
	// up orphaned anyways.
	IsCurrent func() bool
}

type ClientsBlocks struct {
	extraNonce       uint64
	clientBlock      *wire.MsgBlock
	clientBlocksLock sync.Mutex
	lastGenerated    time.Time
	lastTxUpdate     time.Time
	hashesCompleted  uint64
}

type SolvingBlocks struct {
	clientsBlocks     map[string]*ClientsBlocks
	clientsBlocksLock sync.Mutex
}

// DelegateMiner provides facilities for solving blocks (mining) using the CPU in
// a concurrency-safe manner.  It consists of two main goroutines -- a speed
// monitor and a controller for worker goroutines which generate and solve
// blocks.  The number of goroutines can be set via the SetMaxGoRoutines
// function, but the default is based on the number of processor cores in the
// system which is typically sufficient.
type DelegateMiner struct {
	sync.Mutex
	g                 *mining.BlkTmplGenerator
	cfg               Config
	numWorkers        uint32
	started           bool
	discreteMining    bool
	submitBlockLock   sync.Mutex
	wg                sync.WaitGroup
	updateNumWorkers  chan struct{}
	queryHashesPerSec chan float64
	updateHashes      chan uint64

	socketServer      *socketserver.SocketServer
	solvingBlocks     map[int32]*SolvingBlocks
	solvingBlocksLock sync.Mutex
}

// submitBlock submits the passed block to network after ensuring it passes all
// of the consensus validation rules.
func (m *DelegateMiner) submitBlock(block *btcutil.Block) bool {
	m.submitBlockLock.Lock()
	defer m.submitBlockLock.Unlock()

	// Ensure the block is not stale since a new block could have shown up
	// while the solution was being found.  Typically that condition is
	// detected and all work on the stale block is halted to start work on
	// a new block, but the check only happens periodically, so it is
	// possible a block was found and submitted in between.
	msgBlock := block.MsgBlock()
	if !msgBlock.Header.PrevBlock.IsEqual(&m.g.BestSnapshot().Hash) {
		log.Debugf("Block submitted via CPU miner with previous "+
			"block %s is stale", msgBlock.Header.PrevBlock)
		return false
	}

	// Process this block using the same rules as blocks coming from other
	// nodes.  This will in turn relay it to the network like normal.
	isOrphan, err := m.cfg.ProcessBlock(block, blockchain.BFNone)
	if err != nil {
		// Anything other than a rule violation is an unexpected error,
		// so log that error as an internal error.
		if _, ok := err.(blockchain.RuleError); !ok {
			log.Errorf("Unexpected error while processing "+
				"block submitted via CPU miner: %v", err)
			return false
		}

		log.Debugf("Block submitted via CPU miner rejected: %v", err)
		return false
	}
	if isOrphan {
		log.Debugf("Block submitted via CPU miner is an orphan")
		return false
	}

	// The block was accepted.
	coinbaseTx := block.MsgBlock().Transactions[0].TxOut[0]
	log.Infof("Block submitted via CPU miner accepted (hash %s, "+
		"amount %v)", block.Hash(), btcutil.Amount(coinbaseTx.Value))
	return true
}

// Start begins the CPU mining process as well as the speed monitor used to
// track hashing metrics.  Calling this function when the CPU miner has
// already been started will have no effect.
//
// This function is safe for concurrent access.
func (m *DelegateMiner) Start() {
	m.Lock()
	defer m.Unlock()

	// Nothing to do if the miner is already running
	if m.started {
		return
	}
	go m.socketServer.Start(m)

	m.started = true
	log.Infof("Delegate miner started")
}

// Stop gracefully stops the mining process by signalling all workers, and the
// speed monitor to quit.  Calling this function when the CPU miner has not
// already been started will have no effect.
//
// This function is safe for concurrent access.
func (m *DelegateMiner) Stop() {
	m.Lock()
	defer m.Unlock()

	// Nothing to do if the miner is not currently running or if running in
	// discrete mode (using GenerateNBlocks).
	if !m.started {
		return
	}

	m.started = false
	log.Infof("Delegate miner stopped")
}

// IsMining returns whether or not the CPU miner has been started and is
// therefore currenting mining.
//
// This function is safe for concurrent access.
func (m *DelegateMiner) IsMining() bool {
	m.Lock()
	defer m.Unlock()

	return m.started
}

// HashesPerSecond returns the number of hashes per second the mining process
// is performing.  0 is returned if the miner is not currently running.
//
// This function is safe for concurrent access.
func (m *DelegateMiner) HashesPerSecond() float64 {
	m.Lock()
	defer m.Unlock()

	// Nothing to do if the miner is not currently running.
	if !m.started {
		return 0
	}

	return <-m.queryHashesPerSec
}

// New returns a new instance of a CPU miner for the provided configuration.
// Use Start to begin the mining process.  See the documentation for DelegateMiner
// type for more details.
func New(cfg *Config) *DelegateMiner {
	return &DelegateMiner{
		g:                 cfg.BlockTemplateGenerator,
		cfg:               *cfg,
		updateNumWorkers:  make(chan struct{}),
		queryHashesPerSec: make(chan float64),
		updateHashes:      make(chan uint64),
		socketServer:      socketserver.NewSocketServer(),
		solvingBlocks:     make(map[int32]*SolvingBlocks),
	}
}

func (m *DelegateMiner) decodeAddress(address string) (btcutil.Address, error) {
	addr, err := btcutil.DecodeAddress(address, m.cfg.ChainParams)
	if err != nil {
		str := "delegate mining address '%s' failed to decode: %v"
		err := fmt.Errorf(str, address, err)
		fmt.Fprintln(os.Stderr, err)
		return nil, err
	}
	if !addr.IsForNet(m.cfg.ChainParams) {
		str := "delegate mining address '%s' is on the wrong network"
		err := fmt.Errorf(str, address)
		fmt.Fprintln(os.Stderr, err)
		return nil, err
	}
	return addr, nil
}

func (m *DelegateMiner) getCurrentBlockHeight() (int32, error) {

	// Wait until there is a connection to at least one other peer
	// since there is no way to relay a found block or receive
	// transactions to work on when there are no connected peers.
	if m.cfg.ConnectedCount() == 0 {
		return 0, errors.New("no nodes conntected")
	}

	// No point in searching for a solution before the chain is
	// synced.  Also, grab the same lock as used for block
	// submission, since the current block will be changing and
	// this would otherwise end up building a new block template on
	// a block that is in the process of becoming stale.
	m.submitBlockLock.Lock()
	curHeight := m.g.BestSnapshot().Height
	m.submitBlockLock.Unlock()
	if curHeight != 0 && !m.cfg.IsCurrent() {
		return 0, errors.New("waiting for node to catch up to chain tip")
	}

	return curHeight, nil
}

func (m *DelegateMiner) getClientsBlocks(address string, blockHeight int32) *ClientsBlocks {
	m.solvingBlocksLock.Lock()
	defer m.solvingBlocksLock.Unlock()

	solvingBlocks := m.solvingBlocks[blockHeight]
	if solvingBlocks == nil {
		return nil
	}

	solvingBlocks.clientsBlocksLock.Lock()
	defer solvingBlocks.clientsBlocksLock.Unlock()

	clientsBlocks := solvingBlocks.clientsBlocks[address]

	return clientsBlocks
}

func (m *DelegateMiner) isClientsBlocksCurrent(clientsBlocks *ClientsBlocks) bool {
	best := m.g.BestSnapshot()

	// The current block is stale if the best block
	// has changed.
	if !clientsBlocks.clientBlock.Header.PrevBlock.IsEqual(&best.Hash) {
		log.Infof("isClientsBlocksOutdated block is stale")
		return false
	}

	// The current block is stale if the memory pool
	// has been updated since the block template was
	// generated and it has been at least one
	// minute.
	if clientsBlocks.lastTxUpdate != m.g.TxSource().LastUpdated() &&
		time.Now().After(clientsBlocks.lastGenerated.Add(time.Minute)) {
		log.Infof("isClientsBlocksOutdated memory pool updated")
		return false
	}

	return true
}

func (m *DelegateMiner) newClientsBlocksProblem(address string, blockHeight int32) (*ClientsBlocks, error) {

	// Choose a commission address at random.
	rand.Seed(time.Now().UnixNano())
	commissionAddr := m.cfg.DelegateAddrs[rand.Intn(len(m.cfg.DelegateAddrs))]

	delegateAddress, err := m.decodeAddress(address)
	if err != nil {
		return nil, err
	}

	// Create a new block template using the available transactions
	// in the memory pool as a source of transactions to potentially
	// include in the block.
	m.submitBlockLock.Lock()
	template, err := m.g.NewBlockTemplate(delegateAddress, commissionAddr)
	m.submitBlockLock.Unlock()
	if err != nil {
		errStr := fmt.Sprintf("Failed to create new block "+
			"template: %v", err)
		log.Errorf(errStr)
		return nil, err
	}

	clientsBlocks := &ClientsBlocks{
		lastGenerated:   time.Now(),
		lastTxUpdate:    m.g.TxSource().LastUpdated(),
		hashesCompleted: uint64(0),
	}
	clientsBlocks.clientBlock = template.Block

	return clientsBlocks, nil
}

func (m *DelegateMiner) updateClientsBlocksProblem(clientsBlocks *ClientsBlocks, blockHeight int32) error {
	clientsBlocks.clientBlocksLock.Lock()
	defer clientsBlocks.clientBlocksLock.Unlock()

	clientsBlocks.extraNonce = clientsBlocks.extraNonce + 1
	err := m.g.UpdateExtraNonce(clientsBlocks.clientBlock, blockHeight, clientsBlocks.extraNonce)
	if err != nil {
		return err
	}
	err = m.g.UpdateBlockTime(clientsBlocks.clientBlock)
	return err
}

func (m *DelegateMiner) GetHeaderProblem(request *wire.HeaderProblemRequest) (*wire.HeaderProblemResponse, error) {
	log.Infof("GetHeaderProblem ", *request)

	curHeight, err := m.getCurrentBlockHeight()
	if err != nil {
		return nil, err
	}
	nextHeight := curHeight + 1
	var solvingBlocks *SolvingBlocks

	clientsBlocks := m.getClientsBlocks(request.Address, nextHeight)
	if clientsBlocks == nil || !m.isClientsBlocksCurrent(clientsBlocks) {
		clientsBlocks, err = m.newClientsBlocksProblem(request.Address, nextHeight)
		if err != nil {
			return nil, err
		}
		solvingBlocks.clientsBlocks[request.Address] = clientsBlocks
	} else {
		err := m.updateClientsBlocksProblem(clientsBlocks, nextHeight)
		if err != nil {
			return nil, err
		}
	}

	return &wire.HeaderProblemResponse{
		Header:      clientsBlocks.clientBlock.Header,
		Address:     request.Address,
		BlockHeight: nextHeight,
		ExtraNonce:  clientsBlocks.extraNonce,
	}, nil
}

func (m *DelegateMiner) SetHeaderSolution(solution *wire.HeaderSolution) error {
	m.solvingBlocksLock.Lock()
	solvingBlocks := m.solvingBlocks[solution.BlockHeight]
	if solvingBlocks == nil {
		return fmt.Errorf("no solving blocks found for height %v", solution.BlockHeight)
	}
	m.solvingBlocksLock.Unlock()

	solvingBlocks.clientsBlocksLock.Lock()
	clientsBlocks := solvingBlocks.clientsBlocks[solution.Address]
	if clientsBlocks == nil {
		return fmt.Errorf("no client block found for address %v", solution.Address)
	}
	solvingBlocks.clientsBlocksLock.Unlock()

	if clientsBlocks.extraNonce != solution.ExtraNonce {
		return fmt.Errorf("client block extra nonce %v did not match solved extra nonce %v",
			clientsBlocks.extraNonce, solution.ExtraNonce)
	}

	clientsBlocks.clientBlock.Header.Nonce = solution.Nonce

	block := btcutil.NewBlock(clientsBlocks.clientBlock)
	if !m.submitBlock(block) {
		return errors.New("block submit failed")
	}

	return nil
}
