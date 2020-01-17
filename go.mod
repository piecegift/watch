module github.com/piecegift/watch

go 1.13

replace github.com/lightninglabs/neutrino => github.com/yaslama/neutrino v0.11.1-0.20191124151815-9586e92e4feb

require (
	github.com/btcsuite/btcd v0.20.1-beta
	github.com/btcsuite/btclog v0.0.0-20170628155309-84c8d2346e9f
	github.com/btcsuite/btcutil v1.0.1
	github.com/btcsuite/btcwallet/walletdb v1.2.0
	github.com/lightninglabs/neutrino v0.11.0
	github.com/lightningnetwork/lnd v0.8.2-beta
)
