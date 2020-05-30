package watch

import (
	"fmt"
	"log"
	"time"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/rpcclient"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
	"github.com/btcsuite/btcwallet/walletdb"
	"github.com/lightninglabs/neutrino"
)

// FullWatcher downloads all blocks instead of using cfilters.
type FullWatcher struct {
	cs            *neutrino.ChainService
	db            walletdb.DB
	params        *chaincfg.Params
	blockCallback func(*btcutil.Block)
	fullClose     chan struct{}
}

func NewFullWatcher(torSocks string, testnet bool, dir string, blockCallback func(*btcutil.Block)) (*FullWatcher, error) {
	cs, db, params, err := makeService(nil, torSocks, testnet, dir)
	if err != nil {
		return nil, err
	}
	return &FullWatcher{
		cs:            cs,
		db:            db,
		params:        params,
		blockCallback: blockCallback,
		fullClose:     make(chan struct{}),
	}, nil
}

func (w *FullWatcher) Close() error {
	close(w.fullClose)
	if err := w.cs.Stop(); err != nil {
		return err
	}
	if err := w.db.Close(); err != nil {
		return err
	}
	return nil
}

func (w *FullWatcher) WaitForSync() error {
	for !w.cs.IsCurrent() {
		time.Sleep(10 * time.Second)

		header, err := w.cs.BestBlock()
		if err != nil {
			return err
		}
		log.Printf("%d %s", header.Height, header.Hash)
	}
	return nil
}

func (w *FullWatcher) CurrentHeight() (int32, error) {
	header, err := w.cs.BestBlock()
	if err != nil {
		return 0, err
	}
	return header.Height, nil
}

func (w *FullWatcher) StartWatching(startBlock int32, handlers rpcclient.NotificationHandlers) {
	if err := w.WaitForSync(); err != nil {
		panic(err)
	}

	height := startBlock

	go func() {
		for {
			select {
			case <-w.fullClose:
				return
			default:
			}

			if err := w.getBlock(height, handlers); err != nil {
				select {
				case <-w.fullClose:
					return
				default:
				}
				log.Println(err)
				time.Sleep(time.Second)
				continue
			}

			height++
		}
	}()
}

func (w *FullWatcher) getBlock(height int32, handlers rpcclient.NotificationHandlers) error {
	bestHeight, err := w.CurrentHeight()
	if err != nil {
		return fmt.Errorf("BestBlock failed: %w", err)
	}
	if height > bestHeight {
		time.Sleep(time.Second)
		return nil
	}

	blockHash, err := w.cs.GetBlockHash(int64(height))
	if err != nil {
		return fmt.Errorf("GetBlockHash(%d) failed: %w", height, err)
	}
	block, err := w.cs.GetBlock(*blockHash)
	if err != nil {
		return fmt.Errorf("for height %d GetBlock failed: %v.", height, err)
	}
	var header *wire.BlockHeader
	if handlers.OnBlockConnected != nil || handlers.OnFilteredBlockConnected != nil {
		header, err = w.cs.GetBlockHeader(blockHash)
		if err != nil {
			return fmt.Errorf("for height %d GetBlockHeader(%s) failed: %v.", height, blockHash, err)
		}
	}

	if w.blockCallback != nil {
		w.blockCallback(block)
	}
	if handlers.OnBlockConnected != nil {
		handlers.OnBlockConnected(blockHash, height, header.Timestamp)
	}
	if handlers.OnFilteredBlockConnected != nil {
		handlers.OnFilteredBlockConnected(height, header, block.Transactions())
	}

	return nil
}

func (w *FullWatcher) AddAddresses(addrs ...string) error {
	// TODO: implement
	return nil
}
