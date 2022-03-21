// Copyright (C) 2021 Storj Labs, Inc.
// See LICENSE for copying information.

package main

import (
	"context"
	"log"
	"storj.io/storjscan/wallets"

	"github.com/spf13/cobra"

	"storj.io/private/process"
	"storj.io/storjscan/storjscandb"

	"github.com/zeebo/errs"
	"go.uber.org/zap"

	"storj.io/private/cfgstruct"
	"storj.io/storjscan"
)

var (
	rootCmd = &cobra.Command{
		Use:   "storjscan",
		Short: "STORJ token payment management service",
	}

	runCfg storjscan.Config
	runCmd = &cobra.Command{
		Use:   "run",
		Short: "Start payment listener daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, _ := process.Ctx(cmd)
			return run(ctx, runCfg)
		},
	}

	generateCfg wallets.GenerateConfig
	generateCmd = &cobra.Command{
		Use:   "generate",
		Short: "Generated deterministic wallet addresses and register them to the db",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, _ := process.Ctx(cmd)
			return wallets.Generate(ctx, generateCfg.Address, generateCfg.Key)
		},
	}
)

func init() {
	defaults := cfgstruct.DefaultsFlag(rootCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(generateCmd)
	process.Bind(runCmd, &runCfg, defaults)
}

func main() {
	process.ExecCustomDebug(rootCmd)
}

func run(ctx context.Context, config storjscan.Config) error {
	logger := zap.NewExample()
	defer func() {
		if err := logger.Sync(); err != nil {
			log.Println(err)
		}
	}()

	db, err := storjscandb.Open(ctx, logger.Named("storjscandb"), config.Database)
	if err != nil {
		return err
	}

	app, err := storjscan.NewApp(logger.Named("storjscan"), config, db)
	if err != nil {
		return err
	}

	runErr := app.Run(ctx)
	closeErr := app.Close()
	return errs.Combine(runErr, closeErr)
}
