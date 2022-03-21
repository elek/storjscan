package wallets

import (
	"context"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	mm "github.com/miguelmota/go-ethereum-hdwallet"
	"github.com/zeebo/errs"
	"io/ioutil"
	"strings"
)

// GenerateConfig for wallet address generation.
type GenerateConfig struct {
	Address      string `help:"public address to listen on" default:"http://127.0.01:10000"`
	Key          string `help:"Secrets to connect to service endpoints."`
	MnemonicFile string `help:"File which contains the mnemonic to be used for HD generation." default:".mnemonic"`
	Min          int    `help:"Index of the first derived address." default:"0"`
	Max          int    `help:"Index of the last derived address." default:"1000"`
}

// Generate creates and registers new HD wallet addresses.
func Generate(ctx context.Context, config GenerateConfig) error {
	client := NewWalletClient(config.Address, config.Key)
	return generateWithPersistFunc(ctx, config, client.PersistAddresses)
}

func generateWithPersistFunc(ctx context.Context, config GenerateConfig, persist func(context.Context, []common.Address) error) error {

	mnemonic, err := ioutil.ReadFile(config.MnemonicFile)
	if err != nil {
		return errs.New("Couldn't read mnemonic from %s: %v", config.MnemonicFile, err)
	}
	var addr []common.Address

	seed, err := mm.NewSeedFromMnemonic(strings.TrimSpace(string(mnemonic)))
	if err != nil {
		return errs.Wrap(err)
	}

	w, err := mm.NewFromSeed(seed)
	if err != nil {
		return errs.Wrap(err)
	}

	next := accounts.DefaultIterator(mm.DefaultBaseDerivationPath)

	for i := 0; i <= config.Max; i++ {
		path := next()
		if i < config.Min {
			continue
		}
		account, err := w.Derive(path, false)
		if err != nil {
			return errs.Wrap(err)
		}
		addr = append(addr, account.Address)
	}
	return persist(ctx, addr)
}

// Mnemonic generates new mnemonic
func Mnemonic() (string, error) {
	return mm.NewMnemonic(256)
}
