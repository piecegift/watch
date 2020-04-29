package main

import (
	"flag"
	"log"
	"os"
	"os/signal"

	"github.com/btcsuite/btcd/rpcclient"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
	"github.com/piecegift/watch"
)

var (
	testnet      = flag.Bool("testnet", false, "Use testnet instead of mainnet")
	torSocksAddr = flag.String("tor-socks", "127.0.0.1:9050", "Tor address for neutrino")
	addr         = flag.String("address", "", "Address to follow")
	startBlock   = flag.Int("start-block", 0, "Start block")
	dir          = flag.String("dir", ".", "Directory with neutrino data")
)

func main() {
	flag.Parse()

	peers := watch.MainNetPeers
	if *testnet {
		peers = watch.TestNet3Peers
	}

	log.Println("Creating watcher.")
	watcher, err := watch.New(peers, *torSocksAddr, *testnet, *dir)
	if err != nil {
		log.Fatalf("New: %v.", err)
	}

	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt)
	defer func() {
		c <- os.Interrupt
	}()
	go func() {
		<-c
		log.Printf("Closing the watcher.")
		if err := watcher.Close(); err != nil {
			log.Fatalf("Close: %v.", err)
		}
		os.Exit(0)
	}()

	log.Println("Running WaitForSync.")
	if err := watcher.WaitForSync(); err != nil {
		log.Fatalf("WaitForSync: %v.", err)
	}

	height, err := watcher.CurrentHeight()
	if err != nil {
		log.Printf("CurrentHeight: %v.", err)
	}
	log.Printf("Height is %d.", height)

	if *addr == "" {
		return
	}

	log.Printf("Following %s. Incomes only.", *addr)
	handler := func(height int32, header *wire.BlockHeader, relevantTxs []*btcutil.Tx) {
		for _, tx := range relevantTxs {
			outputs := watch.PrepareTxOutputs(tx, *testnet)
			amount, has := outputs[*addr]
			if !has {
				return
			}
			log.Printf("tx %s height %d: +%s.", tx.Hash(), height, amount)
		}
	}
	handlers := rpcclient.NotificationHandlers{
		OnFilteredBlockConnected: handler,
	}
	watcher.StartWatching(int32(*startBlock), handlers)
	if err := watcher.AddAddresses(*addr); err != nil {
		log.Fatalf("AddAddresses: %v.", err)
	}

	select {}
}
