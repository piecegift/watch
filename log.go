package watch

import (
	"os"

	"github.com/btcsuite/btclog"
	"github.com/lightninglabs/neutrino"
)

func EnableNeutrinoLogs(prefix string, level btclog.Level) {
	logger := btclog.NewBackend(os.Stdout)
	chainLogger := logger.Logger(prefix)
	chainLogger.SetLevel(level)
	neutrino.UseLogger(chainLogger)
}
