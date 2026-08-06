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
		ArgsUsage: "<repository>",
		Flags:     []cli.Flag{urlFlag(), architectureFlag()},
		Action:    list,
	}
}

func list(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() != 1 {
		return errors.New("usage: pkgdepot list [options] <repository>")
	}
	client := httpclient.New(cmd.String("url"), "")
	packages, err := client.List(ctx, cmd.Args().First(), cmd.String("architecture"))
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(packages)
}
