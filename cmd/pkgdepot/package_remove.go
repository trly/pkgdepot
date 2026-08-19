package main

import (
	"context"
	"errors"

	"github.com/trly/pkgdepot/internal/httpclient"
	"github.com/urfave/cli/v3"
)

func packageRemoveCommand() *cli.Command {
	return &cli.Command{
		Name:      "remove",
		Usage:     "remove a package",
		ArgsUsage: "<repository> <package-name>",
		Flags:     append(clientFlags(), architectureFlag()),
		Action:    remove,
	}
}

func remove(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() != 2 {
		return errors.New("usage: pkgdepot package remove [options] <repository> <package-name>")
	}
	client := httpclient.New(ctx, cmd.String("url"))
	return client.Remove(ctx, cmd.Args().First(), cmd.String("architecture"), cmd.Args().Get(1))
}
