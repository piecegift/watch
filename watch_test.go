package watch

import (
	"io/ioutil"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/rpcclient"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
)

func TestWatcher(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "watch_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	log.Println("Creating watcher.")
	watcher, err := New(MainNetPeers, "", false, tmpDir)
	if err != nil {
		t.Fatalf("New: %v.", err)
	}

	log.Println("Running WaitForSync.")
	if err := watcher.WaitForSync(); err != nil {
		t.Fatalf("WaitForSync: %v.", err)
	}

	log.Println("Running CurrentHeight.")
	height, err := watcher.CurrentHeight()
	if err != nil {
		t.Errorf("CurrentHeight: %v.", err)
	}
	if height < 600000 {
		t.Errorf("height is %d, want >600k.", height)
	}

	const addr = "3HuJwfCpp3mB8hFctX2N9SMz7euKCQ4vWs"
	const txid = "d40f946de9a47d28f0d706d183186ca84b048080736dee4234f8ea9a06a48c26"
	wantAmount := btcutil.Amount(20731159)

	check := func(watcher *Watcher, startBlock int32, startWatchingBefore bool) {
		var wg sync.WaitGroup
		handler := func(height int32, header *wire.BlockHeader, relevantTxs []*btcutil.Tx) {
			for _, tx := range relevantTxs {
				log.Printf("Found tx %s.", tx.Hash())
				if tx.Hash().String() != txid {
					return
				}
				outputs := PrepareTxOutputs(tx, false)
				if outputs[addr] != wantAmount {
					t.Errorf(
						"Address %s in tx %s got %s, want %s.",
						addr, txid, outputs[addr], wantAmount,
					)
				}
				wg.Done()
			}
		}
		handlers := rpcclient.NotificationHandlers{
			OnBlockConnected: func(hash *chainhash.Hash, height int32, t time.Time) {
				log.Printf("New block: %d.", height)
			},
			OnFilteredBlockConnected: handler,
			OnFilteredBlockDisconnected: func(height int32, header *wire.BlockHeader) {
				log.Println("Block disconnected", height)
			},
		}
		if startWatchingBefore {
			watcher.StartWatching(startBlock, handlers)
		}
		if err := watcher.AddAddresses(addr); err != nil {
			t.Fatalf("AddAddresses: %v.", err)
		}
		if !startWatchingBefore {
			watcher.StartWatching(startBlock, handlers)
		}
		wg.Add(1)
		wg.Wait()
	}

	log.Println("Checking some address.")
	check(watcher, 628320, true)

	log.Println("Closing the watcher.")
	if err := watcher.Close(); err != nil {
		t.Fatalf("Close: %v.", err)
	}

	log.Println("Reopening again and rechecking the same.")
	watcher, err = New(MainNetPeers, "", false, tmpDir)
	if err != nil {
		t.Fatalf("New: %v.", err)
	}
	log.Println("Running WaitForSync.")
	if err := watcher.WaitForSync(); err != nil {
		t.Fatalf("WaitForSync: %v.", err)
	}
	check(watcher, 628320, true)

	log.Println("Closing the watcher.")
	if err := watcher.Close(); err != nil {
		t.Fatalf("Close: %v.", err)
	}

	// https://github.com/lightninglabs/neutrino/pull/194#issuecomment-575613975
	log.Println("Checking for the bug that happens if there is no scanner on the first run or it starts with higher block than a subsequent scanner.")
	watcher, err = New(MainNetPeers, "", false, tmpDir)
	if err != nil {
		t.Fatalf("New: %v.", err)
	}
	log.Println("Running WaitForSync.")
	if err := watcher.WaitForSync(); err != nil {
		t.Fatalf("WaitForSync: %v.", err)
	}
	check(watcher, 628000, true)

	log.Println("Closing the watcher.")
	if err := watcher.Close(); err != nil {
		t.Fatalf("Close: %v.", err)
	}

	log.Println("Now call AddAddresses before StartWatching.")
	watcher, err = New(MainNetPeers, "", false, tmpDir)
	if err != nil {
		t.Fatalf("New: %v.", err)
	}
	log.Println("Running WaitForSync.")
	if err := watcher.WaitForSync(); err != nil {
		t.Fatalf("WaitForSync: %v.", err)
	}
	check(watcher, 628320, false)

	log.Println("Closing the watcher.")
	if err := watcher.Close(); err != nil {
		t.Fatalf("Close: %v.", err)
	}
}
