package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/trly/pkgdepot/internal/httpclient"
	"github.com/urfave/cli/v3"
)

func listCommand() *cli.Command {
	return &cli.Command{
		Name:      "list",
		Usage:     "list packages",
		ArgsUsage: "<repository> <architecture>",
		Flags:     clientFlags(),
		Action:    list,
	}
}

func list(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() != 2 {
		return errors.New("usage: pkgdepot list [options] <repository> <architecture>")
	}
	client := httpclient.New(cmd.String("url"), cmd.String("token"))
	packages, err := client.List(ctx, cmd.Args().Get(0), cmd.Args().Get(1))
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(packages)
}
