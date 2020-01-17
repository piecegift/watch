package watch

import (
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"
	"github.com/btcsuite/btcutil"
)

func PrepareTxOutputs(tx *btcutil.Tx, testnet bool) map[string]btcutil.Amount {
	params := &chaincfg.MainNetParams
	if testnet {
		params = &chaincfg.TestNet3Params
	}

	result := make(map[string]btcutil.Amount)

	for _, txOut := range tx.MsgTx().TxOut {
		pkScript, err := txscript.ParsePkScript(txOut.PkScript)
		if err != nil {
			continue
		}
		a, err := pkScript.Address(params)
		if err != nil {
			continue
		}
		result[a.EncodeAddress()] += btcutil.Amount(txOut.Value)
	}
	return result
}
