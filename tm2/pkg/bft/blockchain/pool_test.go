package blockchain

import (
	"fmt"
	"testing"
	"time"

	p2pTypes "github.com/gnolang/gno/tm2/pkg/p2p/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gnolang/gno/tm2/pkg/bft/types"
	"github.com/gnolang/gno/tm2/pkg/log"
	"github.com/gnolang/gno/tm2/pkg/random"
)

func init() {
	peerTimeout = 2 * time.Second
}

type testPeer struct {
	id        p2pTypes.ID
	height    int64
	inputChan chan inputData // make sure each peer's data is sequential
}

type inputData struct {
	t       *testing.T
	pool    *BlockPool
	request BlockRequest
}

func (p testPeer) runInputRoutine() {
	go func() {
		for input := range p.inputChan {
			p.simulateInput(input)
		}
	}()
}

// Request desired, pretend like we got the block immediately.
func (p testPeer) simulateInput(input inputData) {
	block := &types.Block{Header: types.Header{Height: input.request.Height}}
	input.pool.AddBlock(input.request.PeerID, block, 123)
	// TODO: uncommenting this creates a race which is detected by: https://github.com/golang/go/blob/2bd767b1022dd3254bcec469f0ee164024726486/src/testing/testing.go#L854-L856
	// see: https://github.com/tendermint/classic/issues/3390#issue-418379890
	// input.t.Logf("Added block from peer %v (height: %v)", input.request.PeerID, input.request.Height)
}

type testPeers map[p2pTypes.ID]testPeer

func (ps testPeers) start() {
	for _, v := range ps {
		v.runInputRoutine()
	}
}

func (ps testPeers) stop() {
	for _, v := range ps {
		close(v.inputChan)
	}
}

func makePeers(numPeers int, minHeight, maxHeight int64) testPeers {
	peers := make(testPeers, numPeers)
	for range numPeers {
		peerID := p2pTypes.ID(random.RandStr(12))
		height := minHeight + random.RandInt63n(maxHeight-minHeight)
		peers[peerID] = testPeer{peerID, height, make(chan inputData, 10)}
	}
	return peers
}

func TestBlockPoolBasic(t *testing.T) {
	t.Parallel()

	start := int64(42)
	peers := makePeers(10, start+1, 1000)
	errorsCh := make(chan peerError, 1000)
	requestsCh := make(chan BlockRequest, 1000)
	pool := NewBlockPool(start, requestsCh, errorsCh)
	pool.SetLogger(log.NewNoopLogger())

	err := pool.Start()
	if err != nil {
		t.Error(err)
	}

	defer pool.Stop()

	peers.start()
	defer peers.stop()

	// Introduce each peer.
	go func() {
		for _, peer := range peers {
			pool.SetPeerHeight(peer.id, peer.height)
		}
	}()

	// Start a goroutine to pull blocks
	go func() {
		for {
			if !pool.IsRunning() {
				return
			}
			first, second := pool.PeekTwoBlocks()
			if first != nil && second != nil {
				pool.PopRequest()
			} else {
				time.Sleep(1 * time.Second)
			}
		}
	}()

	// Pull from channels
	for {
		select {
		case err := <-errorsCh:
			t.Error(err)
		case request := <-requestsCh:
			t.Logf("Pulled new BlockRequest %v", request)
			if request.Height == 300 {
				return // Done!
			}

			peers[request.PeerID].inputChan <- inputData{t, pool, request}
		}
	}
}

func TestBlockPoolTimeout(t *testing.T) {
	t.Parallel()

	start := int64(42)
	peers := makePeers(10, start+1, 1000)
	errorsCh := make(chan peerError, 1000)
	requestsCh := make(chan BlockRequest, 1000)
	pool := NewBlockPool(start, requestsCh, errorsCh)
	pool.SetLogger(log.NewTestingLogger(t))
	err := pool.Start()
	if err != nil {
		t.Error(err)
	}
	defer pool.Stop()

	for _, peer := range peers {
		t.Logf("Peer %v", peer.id)
	}

	// Introduce each peer.
	go func() {
		for _, peer := range peers {
			pool.SetPeerHeight(peer.id, peer.height)
		}
	}()

	// Start a goroutine to pull blocks
	go func() {
		for {
			if !pool.IsRunning() {
				return
			}
			first, second := pool.PeekTwoBlocks()
			if first != nil && second != nil {
				pool.PopRequest()
			} else {
				time.Sleep(1 * time.Second)
			}
		}
	}()

	// Pull from channels
	counter := 0
	timedOut := map[p2pTypes.ID]struct{}{}
	for {
		select {
		case err := <-errorsCh:
			t.Log(err)
			// consider error to be always timeout here
			if _, ok := timedOut[err.peerID]; !ok {
				counter++
				if counter == len(peers) {
					return // Done!
				}
			}
		case request := <-requestsCh:
			t.Logf("Pulled new BlockRequest %+v", request)
		}
	}
}

func TestBlockPoolRemovePeer(t *testing.T) {
	t.Parallel()

	peers := make(testPeers, 10)
	for i := range 10 {
		peerID := p2pTypes.ID(fmt.Sprintf("%d", i+1))
		height := int64(i + 1)
		peers[peerID] = testPeer{peerID, height, make(chan inputData)}
	}
	requestsCh := make(chan BlockRequest)
	errorsCh := make(chan peerError)

	pool := NewBlockPool(1, requestsCh, errorsCh)
	pool.SetLogger(log.NewTestingLogger(t))
	err := pool.Start()
	require.NoError(t, err)
	defer pool.Stop()

	// add peers
	for peerID, peer := range peers {
		pool.SetPeerHeight(peerID, peer.height)
	}
	assert.EqualValues(t, 10, pool.MaxPeerHeight())

	// remove not-existing peer
	assert.NotPanics(t, func() { pool.RemovePeer(p2pTypes.ID("Superman")) })

	// remove peer with biggest height
	pool.RemovePeer(p2pTypes.ID("10"))
	assert.EqualValues(t, 9, pool.MaxPeerHeight())

	// remove all peers
	for peerID := range peers {
		pool.RemovePeer(peerID)
	}

	assert.EqualValues(t, 0, pool.MaxPeerHeight())
}
