package main

import (
	"context"
	"errors"

	"github.com/urfave/cli/v3"
)

func tokenRevokeCommand() *cli.Command {
	return &cli.Command{Name: "revoke", Usage: "revoke a local API credential", ArgsUsage: "<id>", Flags: tokenStoreFlags(), Action: tokenRevoke}
}

func tokenRevoke(_ context.Context, cmd *cli.Command) error {
	if cmd.NArg() != 1 {
		return errors.New("usage: pkgdepot token revoke [options] <id>")
	}
	store, err := localTokenStore(cmd)
	if err != nil {
		return err
	}
	return store.Revoke(cmd.Args().First())
}
