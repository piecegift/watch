package watch

import (
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/rpcclient"
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

	// Arguments of New to start from scratch if it breaks.
	peers    []string
	torSocks string
	testnet  bool
	dir      string

	addresses  []string
	fullClose  chan struct{}
	mu         sync.Mutex
	restarting bool
}

func New(peers []string, torSocks string, testnet bool, dir string) (*Watcher, error) {
	watcher := &Watcher{
		peers:    peers,
		torSocks: torSocks,
		testnet:  testnet,
		dir:      dir,

		fullClose: make(chan struct{}),
	}

	if err := watcher.start(); err != nil {
		return nil, err
	}

	return watcher, nil
}

func (w *Watcher) start() error {
	dbFile := filepath.Join(w.dir, "wallet.db")

	var db walletdb.DB
	var err error
	if _, err0 := os.Stat(dbFile); os.IsNotExist(err0) {
		db, err = walletdb.Create("bdb", dbFile, true)
	} else {
		db, err = walletdb.Open("bdb", dbFile, true)
	}
	if err != nil {
		return fmt.Errorf("walletdb: %w", err)
	}

	dataDir := filepath.Join(w.dir, "data")
	if err := os.Mkdir(dataDir, 0700); err != nil && !os.IsExist(err) {
		return fmt.Errorf("Mkdir: %w", err)
	}

	params := chaincfg.MainNetParams
	if w.testnet {
		params = chaincfg.TestNet3Params
	}

	config := neutrino.Config{
		DataDir:       dataDir,
		Database:      db,
		ChainParams:   params,
		AddPeers:      w.peers,
		ConnectPeers:  w.peers,
		PersistToDisk: true, // See https://github.com/lightninglabs/neutrino/pull/194
	}

	if w.torSocks != "" {
		proxy := &tor.ProxyNet{
			SOCKS:           w.torSocks,
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
		return fmt.Errorf("neutrino.NewChainService: %w", err)
	}
	if err := cs.Start(); err != nil {
		return fmt.Errorf("cs.Start: %w", err)
	}

	w.cs = cs
	w.db = db
	w.params = &params

	return nil
}

func (w *Watcher) Close() error {
	close(w.fullClose)
	return w.stop()
}

func (w *Watcher) stop() error {
	if w.quitChan != nil {
		close(w.quitChan)
		w.rescan.WaitForShutdown()
		w.quitChan = nil
		w.rescan = nil
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

func (w *Watcher) StartWatching(startBlock int32, handlers rpcclient.NotificationHandlers) {
	select {
	case <-w.fullClose:
		return
	default:
	}

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
		neutrino.NotificationHandlers(handlers),
	)
	errChan := w.rescan.Start()
	go func() {
		for err := range errChan {
			log.Printf("Rescan error: %v.", err)
			if strings.Contains(err.Error(), "unable to fetch cfilter") {
				log.Println("It looks we have bug https://github.com/lightninglabs/neutrino/pull/194#issuecomment-575613975 here. Restarting neutrino.")
				w.restart(startBlock, handlers)
			}
		}
	}()
}

func (w *Watcher) restart(startBlock int32, handlers rpcclient.NotificationHandlers) {
	w.mu.Lock()
	w.restarting = true
	w.mu.Unlock()

	defer func() {
		w.mu.Lock()
		w.restarting = false
		w.mu.Unlock()
	}()

	if err := w.stop(); err != nil {
		log.Printf("Failed to stop: %v. Giving up.", err)
		return
	}
	dataDir := filepath.Join(w.dir, "data")
	if err := os.RemoveAll(dataDir); err != nil {
		log.Printf("Failed to remove dir %s: %v. Giving up.", dataDir, err)
		return
	}
	dbFile := filepath.Join(w.dir, "wallet.db")
	if err := os.Remove(dbFile); err != nil {
		log.Printf("Failed to remove dbFile %s: %v. Giving up.", dbFile, err)
		return
	}

	if err := w.start(); err != nil {
		log.Printf("Failed to stop: %v. Giving up.", err)
		return
	}
	if err := w.WaitForSync(); err != nil {
		log.Printf("Failed to WaitForSync: %v. Giving up.", err)
		return
	}

	w.StartWatching(startBlock, handlers)

	w.mu.Lock()
	addrs := w.addresses
	w.mu.Unlock()
	if err := w.addAddresses(addrs...); err != nil {
		log.Printf("Failed to add addresses %v: %v. Giving up.", addrs, err)
		return
	}
}

func (w *Watcher) AddAddresses(addrs ...string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.addresses = append(w.addresses, addrs...)
	if w.restarting {
		// We can not add addressed during restarting.
		return nil
	}
	if err := w.addAddresses(addrs...); err != nil {
		return err
	}
	return nil
}

func (w *Watcher) addAddresses(addrs ...string) error {
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
