package loadbot

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"math/rand"
	"time"

	bind "github.com/ethereum/go-ethereum/accounts/abi/bind_v2"
	"github.com/ethereum/go-ethereum/common"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/mycelo/contract"
	"github.com/ethereum/go-ethereum/mycelo/env"
	"golang.org/x/sync/errgroup"
)

const clientCap = 100

type Range struct {
	From *big.Int
	To   *big.Int
}

type LoadBotConfig struct {
	Accounts              []env.Account
	Amount                *big.Int
	TransactionsPerSecond int
	ClientCount           int
	ClientFactory         func() (*ethclient.Client, error)
}

func Start(ctx context.Context, cfg *LoadBotConfig) error {
	group, ctx := errgroup.WithContext(ctx)

	nextTransfer := func() (common.Address, *big.Int) {
		idx := rand.Intn(len(cfg.Accounts))
		return cfg.Accounts[idx].Address, cfg.Amount
	}

	// Use no more than clientCap clients
	clientCount := cfg.ClientCount
	if clientCount > clientCap {
		clientCount = clientCap
	}
	clients := make([]bind.ContractBackend, 0, clientCount)
	for i := 0; i < clientCount; i++ {
		client, err := cfg.ClientFactory()
		if err != nil {
			return err
		}
		clients = append(clients, client)
	}

	// developer accounts / TPS = duration in seconds.
	// Need the fudger factor to get up a consistent TPS at the target.
	delay := time.Duration(int(float64(len(cfg.Accounts)*1000/cfg.TransactionsPerSecond)*0.95)) * time.Millisecond
	startDelay := delay / time.Duration(len(cfg.Accounts))

	for i, acc := range cfg.Accounts {
		// Spread out client load accross different diallers
		client := clients[i%clientCount]

		err := waitFor(ctx, startDelay)
		if err != nil {
			return err
		}
		acc := acc
		group.Go(func() error {
			return runBot(ctx, acc, delay, client, nextTransfer)
		})

	}

	return group.Wait()
}

func runBot(ctx context.Context, acc env.Account, sleepTime time.Duration, client bind.ContractBackend, nextTransfer func() (common.Address, *big.Int)) error {
	abi := contract.AbiFor("StableToken")
	stableToken := bind.NewBoundContract(common.HexToAddress("0xd008"), *abi, client)

	transactor := bind.NewKeyedTransactor(acc.PrivateKey)
	transactor.Context = ctx
	stableTokenAddress := common.HexToAddress("0xd008")
	transactor.FeeCurrency = &stableTokenAddress
	for {
		txSentTime := time.Now()
		recipient, value := nextTransfer()
		tx, err := stableToken.TxObj(transactor, "transferWithComment", recipient, value, "need to proivde some long comment to make it similar to an encrypted comment").Send()
		if err != nil {
			if err != context.Canceled {
				fmt.Printf("Error sending transaction: %v\n", err)
			}
			return fmt.Errorf("Error sending transaction: %w", err)
		}
		// fmt.Printf("cusd transfer generated: from: %s to: %s amount: %s\ttxhash: %s\n", acc.Address.Hex(), recipient.Hex(), value.String(), tx.Transaction.Hash().Hex())

		// printJSON(tx)
		_, err = tx.WaitMined(ctx)
		if err != nil {
			if err != context.Canceled {
				fmt.Printf("Error waiting for tx: %v\n", err)
			}
			return fmt.Errorf("Error waitin for tx: %w", err)
		}

		nextSendTime := txSentTime.Add(sleepTime)
		if time.Now().After(nextSendTime) {
			continue
		}

		err = waitFor(ctx, time.Until(nextSendTime))
		if err != nil {
			return err
		}
	}

}

func waitFor(ctx context.Context, waitTime time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(waitTime):
		return nil
	}
}

func printJSON(obj interface{}) {
	b, _ := json.MarshalIndent(obj, " ", " ")
	fmt.Println(string(b))
}