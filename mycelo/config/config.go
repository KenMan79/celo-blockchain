package config

import (
	"encoding/json"
	"io/ioutil"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/istanbul"
	"github.com/ethereum/go-ethereum/consensus/istanbul/uptime"
	"github.com/ethereum/go-ethereum/params"
)

// Config represents MyCelo configuration parameters
type Config struct {
	ChainID   *big.Int              `json:"chainId"` // chainId identifies the current chain and is used for replay protection
	Istanbul  params.IstanbulConfig `json:"istanbul"`
	Hardforks HardforkConfig        `json:"hardforks"`

	GenesisTimestamp uint64 `json:"genesisTimestamp"`

	Mnemonic           string `json:"mnemonic"`           // Accounts mnemonic
	InitialValidators  int    `json:"initialValidators"`  // Number of initial validators
	ValidatorsPerGroup int    `json:"validatorsPerGroup"` // Number of validators per group in the initial set
	DeveloperAccounts  int    `json:"developerAccounts"`  // Number of developers accounts

	// hydrated field
	GenesisAccounts *GenesisAccounts    `json:"-"`
	ChainConfig     *params.ChainConfig `json:"-"`
}

// HardforkConfig contains celo hardforks activation blocks
type HardforkConfig struct {
	ChurritoBlock *big.Int `json:"churritoBlock"`
	DonutBlock    *big.Int `json:"donutBlock"`
}

func ReadConfig(filepath string) (*Config, error) {
	byteValue, err := ioutil.ReadFile(filepath)
	if err != nil {
		return nil, err
	}

	var cfg Config
	err = json.Unmarshal(byteValue, &cfg)
	if err != nil {
		return nil, err
	}

	cfg.ApplyDefaults()
	cfg.Hydrate()
	return &cfg, nil
}

func WriteConfig(cfg *Config, filepath string) error {
	byteValue, err := json.MarshalIndent(cfg, " ", " ")
	if err != nil {
		return err
	}

	return ioutil.WriteFile(filepath, byteValue, 0644)
}

func DefaultConfig() *Config {
	var cfg Config
	cfg.ApplyDefaults()
	return &cfg
}

func (cfg *Config) ApplyDefaults() {
	if cfg.Hardforks.ChurritoBlock == nil {
		cfg.Hardforks.ChurritoBlock = common.Big0
	}
	if cfg.Hardforks.DonutBlock == nil {
		cfg.Hardforks.DonutBlock = common.Big0
	}

	if cfg.ChainID == nil {
		cfg.ChainID = big.NewInt(10203040)
	}

	if cfg.GenesisTimestamp == 0 {
		cfg.GenesisTimestamp = uint64(time.Now().Unix())
	}

	if cfg.Mnemonic == "" {
		cfg.Mnemonic = MustNewMnemonic()
	}

	if cfg.DeveloperAccounts == 0 {
		cfg.DeveloperAccounts = 10
	}
	if cfg.InitialValidators == 0 {
		cfg.InitialValidators = 3
	}
	if cfg.ValidatorsPerGroup == 0 {
		cfg.ValidatorsPerGroup = 1
	} else {
		if cfg.InitialValidators%cfg.ValidatorsPerGroup != 0 {
			panic("For simplicity. Each ValidatorGroup must have same number of validators")
		}
	}

	if cfg.Istanbul.BlockPeriod == 0 {
		cfg.Istanbul.BlockPeriod = 5
	}
	if cfg.Istanbul.Epoch == 0 {
		cfg.Istanbul.Epoch = 17280
	} else {
		// validate epoch size
		if cfg.Istanbul.Epoch < istanbul.MinEpochSize {
			panic("Invalid epoch size")
		}
	}
	if cfg.Istanbul.LookbackWindow == 0 {
		// Use 12, but take in consideration lookback window range restrictions
		cfg.Istanbul.LookbackWindow = uptime.ComputeLookbackWindow(
			cfg.Istanbul.Epoch,
			12,
			cfg.Hardforks.ChurritoBlock.Cmp(common.Big0) == 0,
			func() (uint64, error) { return 12, nil },
		)

	}
	if cfg.Istanbul.ProposerPolicy == 0 {
		cfg.Istanbul.ProposerPolicy = 2
	}
	if cfg.Istanbul.RequestTimeout == 0 {
		cfg.Istanbul.RequestTimeout = 3000
	}
}

func (cfg *Config) Hydrate() (err error) {
	if cfg.ChainConfig == nil || cfg.GenesisAccounts == nil {
		cfg.ChainConfig = cfg.GenerateChainConfig()
		cfg.GenesisAccounts, err = cfg.GenerateGenesisAccounts()
		return err
	}
	return nil
}

func (cfg *Config) GenerateChainConfig() *params.ChainConfig {
	return &params.ChainConfig{
		ChainID:             cfg.ChainID,
		HomesteadBlock:      big.NewInt(0),
		EIP150Block:         big.NewInt(0),
		EIP150Hash:          common.Hash{},
		EIP155Block:         big.NewInt(0),
		EIP158Block:         big.NewInt(0),
		ByzantiumBlock:      big.NewInt(0),
		ConstantinopleBlock: big.NewInt(0),
		PetersburgBlock:     big.NewInt(0),
		IstanbulBlock:       big.NewInt(0),

		ChurritoBlock: cfg.Hardforks.ChurritoBlock,
		DonutBlock:    cfg.Hardforks.DonutBlock,

		Istanbul: &params.IstanbulConfig{
			Epoch:          cfg.Istanbul.Epoch,
			ProposerPolicy: cfg.Istanbul.ProposerPolicy,
			LookbackWindow: cfg.Istanbul.LookbackWindow,
			BlockPeriod:    cfg.Istanbul.BlockPeriod,
			RequestTimeout: cfg.Istanbul.RequestTimeout,
		},
	}
}

func (cfg *Config) GenerateGenesisAccounts() (*GenesisAccounts, error) {
	admin, err := GenerateAccounts(cfg.Mnemonic, Admin, 1)
	if err != nil {
		return nil, err
	}

	validators, err := GenerateAccounts(cfg.Mnemonic, Validator, cfg.InitialValidators)
	if err != nil {
		return nil, err
	}

	groups, err := GenerateAccounts(cfg.Mnemonic, ValidatorGroup, cfg.InitialValidators/cfg.ValidatorsPerGroup)
	if err != nil {
		return nil, err
	}

	developers, err := GenerateAccounts(cfg.Mnemonic, Developer, cfg.DeveloperAccounts)
	if err != nil {
		return nil, err
	}

	return &GenesisAccounts{
		Admin:           admin[0],
		Validators:      validators,
		ValidatorGroups: groups,
		Developers:      developers,
	}, nil

}
