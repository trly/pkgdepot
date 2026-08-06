package main

import (
	"context"
	"encoding/json"
	"os"

	"github.com/urfave/cli/v3"
)

func tokenListCommand() *cli.Command {
	return &cli.Command{Name: "list", Usage: "list local API credentials", Flags: tokenStoreFlags(), Action: tokenList}
}

func tokenList(_ context.Context, cmd *cli.Command) error {
	store, err := localTokenStore(cmd)
	if err != nil {
		return err
	}
	infos, err := store.List()
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(infos)
}
