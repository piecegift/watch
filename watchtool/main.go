package main

import (
	"flag"
	"log"
	"os"
	"os/signal"

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
	handler := func(tx *btcutil.Tx, confirmed bool) {
		outputs := watch.PrepareTxOutputs(tx, false)
		amount, has := outputs[*addr]
		if !has {
			return
		}
		log.Printf("tx %s (confirmed: %v): +%s BTC.", tx.Hash(), confirmed, amount)
	}
	watcher.StartWatching(int32(*startBlock), handler)
	if err := watcher.AddAddresses(*addr); err != nil {
		log.Fatalf("AddAddresses: %v.", err)
	}

	select {}
}