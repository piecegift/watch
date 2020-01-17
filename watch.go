package watch

import (
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/chaincfg/chainhash"
	"github.com/btcsuite/btcd/rpcclient"
	"github.com/btcsuite/btcd/wire"
	"github.com/btcsuite/btcutil"
	"github.com/btcsuite/btcwallet/walletdb"
	_ "github.com/btcsuite/btcwallet/walletdb/bdb"
	"github.com/lightninglabs/neutrino"
	"github.com/lightninglabs/neutrino/headerfs"
	"github.com/lightningnetwork/lnd/tor"
)

var (
	MainNetPeers = []string{
		"btcd-mainnet.lightning.computer",
		"faucet.lightning.community",
		"mainnet1-btcd.zaphq.io",
		"mainnet2-btcd.zaphq.io",
		"mainnet3-btcd.zaphq.io",
		"mainnet4-btcd.zaphq.io",
	}

	TestNet3Peers = []string{
		"btcd-testnet.lightning.computer",
		"faucet.lightning.community",
		"testnet1-btcd.zaphq.io",
		"testnet2-btcd.zaphq.io",
		"testnet3-btcd.zaphq.io",
		"testnet4-btcd.zaphq.io",
	}
)

type Watcher struct {
	cs *neutrino.ChainService
	db walletdb.DB

	params *chaincfg.Params

	rescan   *neutrino.Rescan
	quitChan chan<- struct{}
}

func New(peers []string, torSocks string, testnet bool, dir string) (*Watcher, error) {
	dbFile := filepath.Join(dir, "wallet.db")

	var db walletdb.DB
	var err error
	if _, err := os.Stat(dbFile); os.IsNotExist(err) {
		db, err = walletdb.Create("bdb", dbFile, true)
	} else {
		db, err = walletdb.Open("bdb", dbFile, true)
	}
	if err != nil {
		return nil, fmt.Errorf("walletdb: %w", err)
	}

	dataDir := filepath.Join(dir, "data")
	if err := os.Mkdir(dataDir, 0700); err != nil && !os.IsExist(err) {
		return nil, fmt.Errorf("Mkdir: %w", err)
	}

	params := chaincfg.MainNetParams
	if testnet {
		params = chaincfg.TestNet3Params
	}

	config := neutrino.Config{
		DataDir:       dataDir,
		Database:      db,
		ChainParams:   params,
		AddPeers:      peers,
		ConnectPeers:  peers,
		PersistToDisk: true, // See https://github.com/lightninglabs/neutrino/pull/194
	}

	if torSocks != "" {
		proxy := &tor.ProxyNet{
			SOCKS:           torSocks,
			StreamIsolation: true,
		}
		config.Dialer = func(addr net.Addr) (net.Conn, error) {
			return proxy.Dial(addr.Network(), addr.String())
		}
		config.NameResolver = func(host string) ([]net.IP, error) {
			return resolveHost(proxy, host)
		}
	}

	cs, err := neutrino.NewChainService(config)
	if err != nil {
		return nil, fmt.Errorf("neutrino.NewChainService: %w", err)
	}
	if err := cs.Start(); err != nil {
		return nil, fmt.Errorf("cs.Start: %w", err)
	}

	watcher := &Watcher{
		cs:     cs,
		db:     db,
		params: &params,
	}

	return watcher, nil
}

func (w *Watcher) Close() error {
	if w.quitChan != nil {
		close(w.quitChan)
		w.rescan.WaitForShutdown()
	}
	if err := w.cs.Stop(); err != nil {
		return err
	}
	if err := w.db.Close(); err != nil {
		return err
	}
	return nil
}

func (w *Watcher) WaitForSync() error {
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

func (w *Watcher) CurrentHeight() (int32, error) {
	header, err := w.cs.BestBlock()
	if err != nil {
		return 0, err
	}
	return header.Height, nil
}

type Handler = func(height int32, header *wire.BlockHeader, relevantTxs []*btcutil.Tx)

func (w *Watcher) StartWatching(startBlock int32, handler Handler) {
	if w.rescan != nil {
		panic("StartWatching called several times")
	}

	quitChan := make(chan struct{})
	w.quitChan = quitChan
	startBlockStamp := &headerfs.BlockStamp{Height: startBlock}
	w.rescan = neutrino.NewRescan(
		&neutrino.RescanChainSource{ChainService: w.cs},
		neutrino.QuitChan(quitChan),
		neutrino.StartBlock(startBlockStamp),
		neutrino.NotificationHandlers(rpcclient.NotificationHandlers{
			OnBlockConnected: func(hash *chainhash.Hash, height int32, t time.Time) {
				log.Printf("New block: %d.", height)
			},
			OnFilteredBlockConnected: handler,
			OnFilteredBlockDisconnected: func(height int32, header *wire.BlockHeader) {
				log.Println("Block disconnected", height)
			},
		}),
	)
	errChan := w.rescan.Start()
	go func() {
		for err := range errChan {
			log.Printf("Rescan error: %v.", err)
		}
	}()
}

func (w *Watcher) AddAddresses(addrs ...string) error {
	aaa := make([]btcutil.Address, 0, len(addrs))
	for _, addr := range addrs {
		a, err := btcutil.DecodeAddress(addr, w.params)
		if err != nil {
			return fmt.Errorf("btcutil.DecodeAddress: %w", err)
		}
		aaa = append(aaa, a)
	}
	if err := w.rescan.Update(neutrino.AddAddrs(aaa...)); err != nil {
		return fmt.Errorf("rescan.Update: %w", err)
	}
	return nil
}

func resolveHost(proxy tor.Net, host string) ([]net.IP, error) {
	addrs, err := proxy.LookupHost(host)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, strIP := range addrs {
		ip := net.ParseIP(strIP)
		if ip == nil {
			continue
		}

		ips = append(ips, ip)
	}
	return ips, nil
}
