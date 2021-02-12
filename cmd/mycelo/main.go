package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/big"
	"os"
	"path"
	"path/filepath"
	"sort"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/internal/fileutils"
	"golang.org/x/sync/errgroup"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/internal/debug"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/mycelo/cluster"
	"github.com/ethereum/go-ethereum/mycelo/config"
	"github.com/ethereum/go-ethereum/mycelo/genesis"
	"github.com/ethereum/go-ethereum/mycelo/loadbot"
	"github.com/ethereum/go-ethereum/params"
	"gopkg.in/urfave/cli.v1"
)

func writeJSON(genesis *core.Genesis, filepath string) error {
	genesisBytes, err := json.Marshal(genesis)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(filepath, genesisBytes, 0644)
}

var (
	// Git information set by linker when building with ci.go.
	gitCommit string
	gitDate   string
	app       = &cli.App{
		Name:        filepath.Base(os.Args[0]),
		Usage:       "mycelo",
		Version:     params.VersionWithCommit(gitCommit, gitDate),
		Writer:      os.Stdout,
		HideVersion: true,
	}
)

func init() {
	// Set up the CLI app.
	app.Flags = append(app.Flags, debug.Flags...)
	app.Before = func(ctx *cli.Context) error {
		return debug.Setup(ctx)
	}
	app.After = func(ctx *cli.Context) error {
		debug.Exit()
		return nil
	}
	app.CommandNotFound = func(ctx *cli.Context, cmd string) {
		fmt.Fprintf(os.Stderr, "No such command: %s\n", cmd)
		os.Exit(1)
	}
	// Add subcommands.
	app.Commands = []cli.Command{
		createGenesisCommand,
		createGenesisConfigCommand,
		createGenesisFromConfigCommand,
		initValidatorsCommand,
		runValidatorsCommand,
		newEnvCommand,
		initNodesCommand,
		runCommand,
		feelingLuckyCommand,
		loadBotCommand,
	}
}

func main() {
	exit(app.Run(os.Args))
}

var cfgOverrideFlags = []cli.Flag{
	cli.IntFlag{
		Name:  "validators",
		Usage: "Number of Validators",
	},
	cli.IntFlag{
		Name:  "dev.accounts",
		Usage: "Number of developer accounts",
	},
	cli.Uint64Flag{
		Name:  "blockperiod",
		Usage: "Seconds between each block",
	},
	cli.Uint64Flag{
		Name:  "epoch",
		Usage: "Epoch size",
	},
	cli.StringFlag{
		Name:  "mnemonic",
		Usage: "Mnemonic to generate accounts",
	},
}

var feelingLuckyCommand = cli.Command{
	Name:   "feeling-lucky",
	Usage:  "Creates, Configure and Run celo blockchain on a single step",
	Action: feelingLucky,
	Flags: append([]cli.Flag{
		cli.StringFlag{
			Name:  "buildpath",
			Usage: "Directory where smartcontract truffle build file live",
		},
		cli.StringFlag{
			Name:  "geth",
			Usage: "Path to geth binary",
		},
	},
		cfgOverrideFlags...),
}

var newEnvCommand = cli.Command{
	Name:   "newenv",
	Usage:  "Creates a new mycelo environment",
	Action: newEnv,
	Flags: append([]cli.Flag{
		cli.StringFlag{
			Name:  "buildpath",
			Usage: "Directory where smartcontract truffle build file live",
		},
	},
		cfgOverrideFlags...),
}

var createGenesisCommand = cli.Command{
	Name:   "genesis",
	Usage:  "Creates genesis.json from a template and overrides",
	Action: createGenesis,
	Flags: append([]cli.Flag{
		cli.StringFlag{
			Name:  "buildpath",
			Usage: "Directory where smartcontract truffle build file live",
		},
		cli.StringFlag{
			Name:  "template",
			Usage: "Optional template to use (default: local)",
		},
	},
		cfgOverrideFlags...),
}

var createGenesisFromConfigCommand = cli.Command{
	Name:   "genesis-from-config",
	Usage:  "Creates genesis.json from a genesis-config.json and env.json",
	Action: createGenesisFromConfig,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "buildpath",
			Usage: "Directory where smartcontract truffle build file live",
		},
	},
}

var createGenesisConfigCommand = cli.Command{
	Name:   "genesis-config",
	Usage:  "Creates genesis-config.json from a template and overrides",
	Action: createGenesisConfig,
	Flags: append([]cli.Flag{
		cli.StringFlag{
			Name:  "template",
			Usage: "Optional template to use (default: local)",
		},
	},
		cfgOverrideFlags...),
}

var initValidatorsCommand = cli.Command{
	Name:   "validator-init",
	Usage:  "Setup all validators nodes",
	Action: initNodes,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "geth",
			Usage: "Path to geth binary",
		},
	},
}

var runValidatorsCommand = cli.Command{
	Name:   "validator-run",
	Usage:  "Runs the testnet",
	Action: run,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "geth",
			Usage: "Path to geth binary",
		},
	},
}

var initNodesCommand = cli.Command{
	Name:   "init-nodes",
	Usage:  "Setup all validators nodes",
	Action: initNodes,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "geth",
			Usage: "Path to geth binary",
		},
	},
}

var runCommand = cli.Command{
	Name:   "run",
	Usage:  "Runs the testnet",
	Action: run,
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "geth",
			Usage: "Path to geth binary",
		},
	},
}

var loadBotCommand = cli.Command{
	Name:   "load-bot",
	Usage:  "Runs the load bot on the environment",
	Action: loadBot,
	Flags:  []cli.Flag{
		// cli.StringFlag{
		// 	Name:  "geth",
		// 	Usage: "Path to geth binary",
		// },
	},
}

func readWorkdir(ctx *cli.Context) (string, error) {
	if ctx.NArg() != 1 {
		return "", fmt.Errorf("Missing directory argument")
	}
	return ctx.Args()[0], nil
}

func readEnv(ctx *cli.Context) (*config.Environment, error) {
	workdir, err := readWorkdir(ctx)
	if err != nil {
		return nil, err
	}
	return config.ReadEnv(workdir)
}

func envFromTemplate(ctx *cli.Context) (*config.Environment, error) {
	workdir, err := readWorkdir(ctx)
	if err != nil {
		workdir = "."
	} else {
		if !fileutils.FileExists(workdir) {
			os.MkdirAll(workdir, os.ModePerm)
		}
	}
	templateString := ctx.String("template")
	env, err := templateFromString(templateString).createEnv(workdir)
	if err != nil {
		return nil, err
	}
	// Overrides
	if ctx.IsSet("epoch") {
		env.GenesisConfig.Istanbul.Epoch = ctx.Uint64("epoch")
	}
	if ctx.IsSet("blockperiod") {
		env.GenesisConfig.Istanbul.BlockPeriod = ctx.Uint64("blockperiod")
	}
	if ctx.IsSet("validators") {
		env.EnvConfig.InitialValidators = ctx.Int("validators")
	}
	if ctx.IsSet("dev.accounts") {
		env.EnvConfig.DeveloperAccounts = ctx.Int("dev.accounts")
	}
	if ctx.IsSet("mnemonic") {
		env.EnvConfig.Mnemonic = ctx.String("mnemonic")
	}
	return env, nil
}

func readBuildPath(ctx *cli.Context) (string, error) {
	buildpath := ctx.String("buildpath")
	if buildpath == "" {
		buildpath = path.Join(os.Getenv("CELO_MONOREPO"), "packages/protocol/build/contracts")
		if fileutils.FileExists(buildpath) {
			log.Info("Missing --buildpath flag, using CELO_MONOREPO derived path", "buildpath", buildpath)
		} else {
			return "", fmt.Errorf("Missing --buildpath flag")
		}
	}
	return buildpath, nil
}

func readGethPath(ctx *cli.Context) (string, error) {
	buildpath := ctx.String("geth")
	if buildpath == "" {
		buildpath = path.Join(os.Getenv("CELO_BLOCKCHAIN"), "build/bin/geth")
		if fileutils.FileExists(buildpath) {
			log.Info("Missing --geth flag, using CELO_BLOCKCHAIN derived path", "geth", buildpath)
		} else {
			return "", fmt.Errorf("Missing --geth flag")
		}
	}
	return buildpath, nil
}

func feelingLucky(ctx *cli.Context) error {
	err := newEnv(ctx)
	if err != nil {
		return err
	}

	env, err := readEnv(ctx)
	if err != nil {
		return err
	}

	env.Paths.Geth, err = readGethPath(ctx)
	if err != nil {
		return err
	}

	cluster := cluster.New(env)

	if err = cluster.Init(); err != nil {
		return err
	}

	runCtx := context.Background()
	group, runCtx := errgroup.WithContext(runCtx)

	group.Go(func() error { return cluster.Run(runCtx) })
	// group.Go(func() error { return cluster.RunMigrations() })

	return group.Wait()
}

func newEnv(ctx *cli.Context) error {
	workdir, err := readWorkdir(ctx)
	if err != nil {
		return err
	}

	template := templateFromString("local")

	log.Info("Creating new environment", "envdir", workdir)
	env, err := template.createEnv(workdir)
	if err != nil {
		return err
	}

	if err := env.WriteGenesisConfig(); err != nil {
		return err
	}

	// Generate genesis block
	buildpath, err := readBuildPath(ctx)
	if err != nil {
		return err
	}

	genesis, err := genesis.GenerateGenesis(env, buildpath)
	if err != nil {
		return err
	}

	return writeJSON(genesis, env.Paths.GenesisJSON())
}

func createGenesis(ctx *cli.Context) error {
	env, err := envFromTemplate(ctx)
	if err != nil {
		return err
	}

	buildpath, err := readBuildPath(ctx)
	if err != nil {
		return err
	}

	genesis, err := genesis.GenerateGenesis(env, buildpath)
	if err != nil {
		return err
	}

	if envdir := ctx.String("newenv"); envdir != "" || ctx.NArg() == 1 {
		err = env.WriteEnvConfig()
		if err != nil {
			return err
		}
	}

	return writeJSON(genesis, env.Paths.GenesisJSON())
}

func createGenesisConfig(ctx *cli.Context) error {
	env, err := envFromTemplate(ctx)
	if err != nil {
		return err
	}

	err = env.WriteEnvConfig()
	if err != nil {
		return err
	}

	return env.WriteGenesisConfig()
}

func createGenesisFromConfig(ctx *cli.Context) error {
	envdir, err := readWorkdir(ctx)
	if err != nil {
		envdir = "."
	}
	env, err := config.ReadBuildEnv(envdir)
	if err != nil {
		return err
	}

	buildpath, err := readBuildPath(ctx)
	if err != nil {
		return err
	}

	genesis, err := genesis.GenerateGenesis(env, buildpath)
	if err != nil {
		return err
	}

	return writeJSON(genesis, env.Paths.GenesisJSON())
}

func validatorInit(ctx *cli.Context) error {
	env, err := readEnv(ctx)
	if err != nil {
		return err
	}

	env.Paths.Geth, err = readGethPath(ctx)
	if err != nil {
		return err
	}

	cluster := cluster.New(env)
	return cluster.Init()
}

func validatorRun(ctx *cli.Context) error {
	env, err := readEnv(ctx)
	if err != nil {
		return err
	}

	env.Paths.Geth, err = readGethPath(ctx)
	if err != nil {
		return err
	}

	cluster := cluster.New(env)

	runCtx := context.Background()
	group, runCtx := errgroup.WithContext(runCtx)

	group.Go(func() error { return cluster.Run(runCtx) })
	return group.Wait()
}

func initNodes(ctx *cli.Context) error {
	env, err := readEnv(ctx)
	if err != nil {
		return err
	}

	env.Paths.Geth, err = readGethPath(ctx)
	if err != nil {
		return err
	}

	cluster := cluster.New(env)
	return cluster.Init()
}

func run(ctx *cli.Context) error {
	env, err := readEnv(ctx)
	if err != nil {
		return err
	}

	env.Paths.Geth, err = readGethPath(ctx)
	if err != nil {
		return err
	}

	cluster := cluster.New(env)

	runCtx := context.Background()
	group, runCtx := errgroup.WithContext(runCtx)

	group.Go(func() error { return cluster.Run(runCtx) })
	// group.Go(func() error { return cluster.RunMigrations() })

	return group.Wait()
}

func loadBot(ctx *cli.Context) error {
	env, err := readEnv(ctx)
	if err != nil {
		return err
	}

	runCtx := context.Background()

	return loadbot.Start(runCtx, &loadbot.LoadBotConfig{
		Accounts:         env.DeveloperAccounts(),
		Amount:           big.NewInt(10000000),
		TransactionDelay: 1 * time.Second,
		ClientFactory: func() (*ethclient.Client, error) {
			return ethclient.Dial("http://localhost:8545")
		},
	})
}

// commandHasFlag returns true if the current command supports the given flag.
func commandHasFlag(ctx *cli.Context, flag cli.Flag) bool {
	flags := ctx.FlagNames()
	sort.Strings(flags)
	i := sort.SearchStrings(flags, flag.GetName())
	return i != len(flags) && flags[i] == flag.GetName()
}

func exit(err interface{}) {
	if err == nil {
		os.Exit(0)
	}
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
