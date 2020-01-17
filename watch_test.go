package watch

import (
	"io/ioutil"
	"log"
	"os"
	"sync"
	"testing"

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

	const addr = "176SsYVUoBcp3kzaBob7b6ikT5MKC16Rz1"
	const txid = "e42efe31d140e15c37ee6bc71d261b67e94d7aff2367d1ef76910376ca0c5f32"
	wantAmount := btcutil.Amount(2730)

	check := func(watcher *Watcher) {
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
		watcher.StartWatching(613000, handler)
		if err := watcher.AddAddresses(addr); err != nil {
			t.Fatalf("AddAddresses: %v.", err)
		}
		wg.Add(1)
		wg.Wait()
	}

	log.Println("Checking some address.")
	check(watcher)

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
	check(watcher)
	log.Println("Closing the watcher.")
	if err := watcher.Close(); err != nil {
		t.Fatalf("Close: %v.", err)
	}
}
