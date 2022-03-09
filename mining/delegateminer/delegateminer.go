// Copyright (c) 2014-2016 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package delegateminer

import (
	"errors"
	"fmt"
	"github.com/btcsuite/btcd/database/delegate"
	"github.com/gorilla/websocket"
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

	blockUpdateSecs = 30

	// hpsUpdateSecs is the number of seconds to wait in between each
	// update to the hashes per second monitor.
	hpsUpdateSecs = 10

	// hashUpdateSec is the number of seconds each worker waits in between
	// notifying the speed monitor with how many hashes have been completed
	// while they are actively searching for a solution.  This is done to
	// reduce the amount of syncs between the workers that must be done to
	// keep track of the hashes per second.
	hashUpdateSecs = 60 * 5
)

// Config is a descriptor containing the cpu miner configuration.
type Config struct {
	// ChainParams identifies which chain parameters the cpu miner is
	// associated with.
	ChainParams *chaincfg.Params

	// BlockTemplateGenerator identifies the instance to use in order to
	// generate block templates that the miner will attempt to solve.
	BlockTemplateGenerator *mining.BlkTmplGenerator

	// CommissionAddrs is a list of payment addresses to use for the generated
	// blocks in the delegate miner.
	// Each generated block will randomly choose one of them.
	CommissionAddrs []btcutil.Address

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

type HeaderSender interface {
	SendHeaderProblem(headerProblem *wire.HeaderProblemResponse, conn *websocket.Conn) error
	SendHeaderSolution(headerSolution *wire.HeaderSolution, conn *websocket.Conn) error
}

// DelegateWorker keeps track of a delegated worker solving blocks for a specific address
type DelegateWorker struct {
	address         string
	blockHeight     int32
	extraNonce      uint64
	block           *wire.MsgBlock
	blockLock       sync.Mutex
	lastGenerated   time.Time
	lastTxUpdate    time.Time
	hashesCompleted uint64
	conn            *websocket.Conn
	//stop            chan struct{}
	quit     chan struct{}
	stopping bool
}

// DelegateMiner provides facilities for solving blocks (mining) using the CPU in
// a concurrency-safe manner.  It consists of two main goroutines -- a speed
// monitor and a controller for worker goroutines which generate and solve
// blocks.  The number of goroutines can be set via the SetMaxGoRoutines
// function, but the default is based on the number of processor cores in the
// system which is typically sufficient.
type DelegateMiner struct {
	sync.Mutex
	g                   *mining.BlkTmplGenerator
	cfg                 Config
	numDelegates        uint32
	started             bool
	submitBlockLock     sync.Mutex
	wg                  sync.WaitGroup
	delegatesWg         sync.WaitGroup
	addDelegateWorker   chan DelegateWorker
	queryHashesPerSec   chan float64
	updateHashes        chan uint64
	quit                chan struct{}
	delegateWorkers     map[*websocket.Conn]*DelegateWorker
	delegateWorkersLock sync.Mutex
	solvingBlocksLock   sync.Mutex
	headerSender        HeaderSender
}

// speedMonitor handles tracking the number of hashes per second the mining
// process is performing.  It must be run as a goroutine.
func (m *DelegateMiner) speedMonitor() {
	log.Infof("Delegate miner speed monitor started")

	var hashesPerSec float64
	var totalHashes uint64
	ticker := time.NewTicker(time.Second * hpsUpdateSecs)
	defer ticker.Stop()

out:
	for {
		select {
		// Periodic updates from the workers with how many hashes they
		// have performed.
		case numHashes := <-m.updateHashes:
			totalHashes += numHashes

		// Time to update the hashes per second.
		case <-ticker.C:
			curHashesPerSec := float64(totalHashes) / hpsUpdateSecs
			if hashesPerSec == 0 {
				hashesPerSec = curHashesPerSec
			}
			hashesPerSec = (hashesPerSec + curHashesPerSec) / 2
			totalHashes = 0
			//if hashesPerSec != 0 {
			log.Infof("Hash speed: %6.0f kilohashes/s",
				hashesPerSec/1000)
			//}

		// Request for the number of hashes per second.
		case m.queryHashesPerSec <- hashesPerSec:
			// Nothing to do.
			time.Sleep(time.Second)

		case <-m.quit:
			log.Infof("Speedmonitor received quit command")
			break out
		}
	}

	m.wg.Done()
	log.Infof("Delegate miner speed monitor done")
}

// submitBlock submits the passed block to network after ensuring it passes all
// of the consensus validation rules.
func (m *DelegateMiner) submitBlock(block *btcutil.Block) bool {
	m.submitBlockLock.Lock()
	defer m.submitBlockLock.Unlock()

	log.Infof("Submitting solved block: %v", block.MsgBlock().Header)

	// Ensure the block is not stale since a new block could have shown up
	// while the solution was being found.  Typically that condition is
	// detected and all work on the stale block is halted to start work on
	// a new block, but the check only happens periodically, so it is
	// possible a block was found and submitted in between.
	msgBlock := block.MsgBlock()
	if !msgBlock.Header.PrevBlock.IsEqual(&m.g.BestSnapshot().Hash) {
		log.Warnf("Block submitted via CPU miner with previous "+
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
				"block submitted via Delegate miner: %v", err)
			return false
		}

		log.Warnf("Block submitted via Delegate miner rejected: %v", err)
		return false
	}
	if isOrphan {
		log.Warnf("Block submitted via Delegate miner is an orphan")
		return false
	}

	// The block was accepted.
	coinbaseTx := block.MsgBlock().Transactions[0].TxOut[0]
	log.Infof("Block submitted via Delegate miner accepted (hash %s, "+
		"amount %v)", block.Hash(), btcutil.Amount(coinbaseTx.Value))
	return true
}

// delegateBlockSolver is a worker that is controlled by the miningWorkerController.
// It is self contained in that it creates block templates and attempts to solve
// them while detecting when it is performing stale work and reacting
// accordingly by generating a new block template.  When a block is solved, it
// is submitted.
//
// It must be run as a goroutine.
func (m *DelegateMiner) delegateBlockSolver(delegateWorker *DelegateWorker) {
	log.Infof("Starting delegate worker for %s", delegateWorker.address)

	delegateWorker.stopping = false

	// Start a ticker which is used to signal checks for stale work
	blockUpdateTicker := time.NewTicker(time.Second * blockUpdateSecs)
	defer blockUpdateTicker.Stop()

	// Start a ticker which is used to signal updates to hashrate
	hashUpdateTicker := time.NewTicker(time.Second * hashUpdateSecs)
	defer hashUpdateTicker.Stop()
out:
	for {

		// Quit when the miner is stopped.
		select {
		//case <-delegateWorker.stop:
		//	log.Infof("delegateBlockSolver got stop command")
		//	delegateWorker.stopping = true
		case <-delegateWorker.quit:
			log.Infof("delegateBlockSolver got quit command")
			break out
		case <-blockUpdateTicker.C:

			log.Infof("delegateBlockSolver blockUpdateTicker")

			if !delegateWorker.stopping && !m.isDelegateBlockCurrent(delegateWorker) {
				log.Infof("delegateBlockSolver not current")
				err := m.newBlockProblem(delegateWorker)
				if err != nil {
					break out
				}

				log.Infof("delegateBlockSolver sending new problem")
				m.headerSender.SendHeaderProblem(&wire.HeaderProblemResponse{
					Header:      delegateWorker.block.Header,
					Address:     delegateWorker.address,
					BlockHeight: delegateWorker.blockHeight,
					ExtraNonce:  delegateWorker.extraNonce,
				}, delegateWorker.conn)
			}

		//m.g.UpdateBlockTime(delegateWorker.block)

		case <-hashUpdateTicker.C:

			log.Infof("delegateBlockSolver hashUpdateTicker")
			hashRates, err := delegate.NewHashrates()
			if err != nil {
				break out
			}

			hashesPerSecond := delegateWorker.hashesCompleted / hashUpdateSecs

			hashRate := delegate.Hashrate{
				Address:    delegateWorker.address,
				MeasuredAt: time.Now(),
				Hashrate:   hashesPerSecond,
			}

			err = hashRates.Insert(hashRate)
			hashRates.Close()
			if err != nil || delegateWorker.stopping {
				break out
			}

		default:
			// Non-blocking select to fall through
			time.Sleep(time.Second)
		}
	}

	log.Infof("delegateBlockSolver stopping...")
	m.delegateWorkersLock.Lock()
	delete(m.delegateWorkers, delegateWorker.conn)
	m.delegateWorkersLock.Unlock()
	m.delegatesWg.Done()
	log.Tracef("Delegate worker done for %s", delegateWorker.address)
}

// NumWorkers returns the number of workers which are running to solve blocks.
//
// This function is safe for concurrent access.
func (m *DelegateMiner) NumWorkers() int32 {
	m.Lock()
	defer m.Unlock()

	return int32(m.numDelegates)
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
	m.quit = make(chan struct{})
	//m.wg.Add(1)
	//go m.speedMonitor()

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
	log.Infof("closing quit")
	close(m.quit)
	//m.delegateWorkersLock.Lock()
	for _, delegateWorker := range m.delegateWorkers {

		log.Infof("closing delegate quit")
		close(delegateWorker.quit)
	}
	//m.delegateWorkersLock.Unlock()

	log.Infof("waiting for delegates to quit")
	m.delegatesWg.Wait()
	m.wg.Wait()
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
func (m *DelegateMiner) SetHeaderSender(headerSender HeaderSender) {
	m.headerSender = headerSender
}

// New returns a new instance of a CPU miner for the provided configuration.
// Use Start to begin the mining process.  See the documentation for DelegateMiner
// type for more details.
func New(cfg *Config) *DelegateMiner {
	return &DelegateMiner{
		g:                 cfg.BlockTemplateGenerator,
		cfg:               *cfg,
		queryHashesPerSec: make(chan float64),
		updateHashes:      make(chan uint64),
		delegateWorkers:   make(map[*websocket.Conn]*DelegateWorker),
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
	log.Infof("getCurrentBlockHeight")

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

func (m *DelegateMiner) isDelegateBlockCurrent(delegateWorker *DelegateWorker) bool {
	best := m.g.BestSnapshot()

	// The current block is stale if the best block
	// has changed.
	if !delegateWorker.block.Header.PrevBlock.IsEqual(&best.Hash) {
		log.Infof("isClientsBlocksOutdated block is stale")
		return false
	}

	// The current block is stale if the memory pool
	// has been updated since the block template was
	// generated and it has been at least one
	// minute.
	if delegateWorker.lastTxUpdate != m.g.TxSource().LastUpdated() &&
		time.Now().After(delegateWorker.lastGenerated.Add(time.Minute)) {
		log.Infof("isClientsBlocksOutdated memory pool updated")
		return false
	}

	return true
}

func (m *DelegateMiner) newBlockProblem(delegateWorker *DelegateWorker) error {
	log.Infof("newBlockProblem for %v", delegateWorker.address)

	// Choose a commission address at random.
	rand.Seed(time.Now().UnixNano())
	commissionAddr := m.cfg.CommissionAddrs[rand.Intn(len(m.cfg.CommissionAddrs))]

	delegateAddress, err := m.decodeAddress(delegateWorker.address)
	if err != nil {
		return err
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
		return err
	}

	delegateWorker.blockLock.Lock()
	defer delegateWorker.blockLock.Unlock()

	delegateWorker.lastGenerated = time.Now()
	delegateWorker.lastTxUpdate = m.g.TxSource().LastUpdated()
	delegateWorker.block = template.Block

	return nil
}

func (m *DelegateMiner) updateBlockProblem(delegateWorker *DelegateWorker, blockHeight int32) error {
	log.Infof("updateBlockProblem at height %v", blockHeight)

	enOffset, err := wire.RandomUint64()
	if err != nil {
		log.Errorf("Unexpected error while generating random extra nonce offset: %v", err)
		enOffset = 1
	}
	delegateWorker.blockLock.Lock()
	defer delegateWorker.blockLock.Unlock()

	delegateWorker.extraNonce = delegateWorker.extraNonce + enOffset
	err = m.g.UpdateExtraNonce(delegateWorker.block, blockHeight, delegateWorker.extraNonce)
	if err != nil {
		return err
	}
	err = m.g.UpdateBlockTime(delegateWorker.block)
	return err
}

func (m *DelegateMiner) GetHeaderProblem(request *wire.HeaderProblemRequest, conn *websocket.Conn) error {
	log.Infof("GetHeaderProblem ", request)

	// Wait until there is a connection to at least one other peer
	// since there is no way to relay a found block or receive
	// transactions to work on when there are no connected peers.

	if m.cfg.ConnectedCount() == 0 {
		return errors.New("no nodes connected")
	}

	// No point in searching for a solution before the chain is
	// synced.  Also, grab the same lock as used for block
	// submission, since the current block will be changing and
	// this would otherwise end up building a new block template on
	// a block that is in the process of becoming stale.

	m.submitBlockLock.Lock()
	curHeight := m.g.BestSnapshot().Height
	if curHeight != 0 && !m.cfg.IsCurrent() {
		m.submitBlockLock.Unlock()
		return errors.New("not synced")
	}
	m.submitBlockLock.Unlock()
	//
	//if request.HashesCompleted > 0 {
	//	m.updateHashes <- request.HashesCompleted * 2
	//}

	curHeight, err := m.getCurrentBlockHeight()
	if err != nil {
		return err
	}
	nextHeight := curHeight + 1

	m.delegateWorkersLock.Lock()
	defer m.delegateWorkersLock.Unlock()

	delegateWorker := m.delegateWorkers[conn]
	if delegateWorker == nil {
		delegateWorker = &DelegateWorker{
			address:     request.Address,
			blockHeight: nextHeight,
			conn:        conn,
			//stop:        make(chan struct{}),
			quit: make(chan struct{}),
		}
		m.delegateWorkers[conn] = delegateWorker

		err = m.newBlockProblem(delegateWorker)
		if err != nil {
			return err
		}
		err = m.updateBlockProblem(delegateWorker, nextHeight)
		if err != nil {
			return err
		}
		log.Infof("GetHeaderProblem starting delegateWorker")
		m.delegatesWg.Add(1)
		go m.delegateBlockSolver(delegateWorker)
	} else {
		delegateWorker.hashesCompleted = delegateWorker.hashesCompleted + request.HashesCompleted
		delegateWorker.stopping = false
		err = m.updateBlockProblem(delegateWorker, nextHeight)
		if err != nil {
			return err
		}
	}

	fmt.Printf("GetHeaderProblem sending problem for worker %v", delegateWorker)

	m.headerSender.SendHeaderProblem(&wire.HeaderProblemResponse{
		Header:      delegateWorker.block.Header,
		Address:     request.Address,
		BlockHeight: nextHeight,
		ExtraNonce:  delegateWorker.extraNonce,
	}, conn)

	return nil
}

func (m *DelegateMiner) SetHeaderSolution(solution *wire.HeaderSolution, conn *websocket.Conn) error {

	m.delegateWorkersLock.Lock()
	defer m.delegateWorkersLock.Unlock()

	delegateWorker := m.delegateWorkers[conn]
	if delegateWorker == nil {
		return fmt.Errorf("SetHeaderSolution no delegate worker found for connection")
	}

	if delegateWorker.extraNonce != solution.ExtraNonce {
		return fmt.Errorf("SetHeaderSolution delegate extra nonce %v did not match solved extra nonce %v",
			delegateWorker.extraNonce, solution.ExtraNonce)
	}

	delegateWorker.block.Header.Nonce = solution.Nonce

	block := btcutil.NewBlock(delegateWorker.block)
	if !m.submitBlock(block) {
		return errors.New("block submit failed")
	}

	solution.BlockHash = block.Hash().String()

	m.headerSender.SendHeaderSolution(solution, conn)

	return nil
}

func (m *DelegateMiner) SocketClosed(conn *websocket.Conn) error {
	//m.delegateWorkersLock.Lock()
	//defer m.delegateWorkersLock.Unlock()

	delegateWorker := m.delegateWorkers[conn]
	if delegateWorker == nil {
		return fmt.Errorf("SocketClosed no delegate worker found for connection")
	}

	delegateWorker.stopping = true
	//close(delegateWorker.stop)
	return nil
}

func (m *DelegateMiner) StopMining(conn *websocket.Conn) error {

	delegateWorker := m.delegateWorkers[conn]
	if delegateWorker == nil {
		return fmt.Errorf("SocketClosed no delegate worker found for connection")
	}

	delegateWorker.stopping = true
	//close(delegateWorker.stop)
	return nil
}
