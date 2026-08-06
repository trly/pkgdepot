package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/trly/pkgdepot/internal/token"
	"github.com/urfave/cli/v3"
)

func tokenRotateCommand() *cli.Command {
	return &cli.Command{Name: "rotate", Usage: "rotate a local API credential", ArgsUsage: "<id>", Flags: tokenStoreFlags(), Action: tokenRotate}
}

func tokenRotate(_ context.Context, cmd *cli.Command) error {
	if cmd.NArg() != 1 {
		return errors.New("usage: pkgdepot token rotate [options] <id>")
	}
	store, err := localTokenStore(cmd)
	if err != nil {
		return err
	}
	info, credential, err := store.Rotate(cmd.Args().First())
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		token.Info
		Credential string `json:"credential"`
	}{Info: info, Credential: credential})
}
