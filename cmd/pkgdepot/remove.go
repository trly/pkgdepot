package main

import (
	"context"
	"errors"

	"github.com/trly/pkgdepot/internal/httpclient"
	"github.com/urfave/cli/v3"
)

func removeCommand() *cli.Command {
	return &cli.Command{
		Name:      "remove",
		Usage:     "remove a package",
		ArgsUsage: "<repository> <architecture> <package-name>",
		Flags:     clientFlags(),
		Action:    remove,
	}
}

func remove(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() != 3 {
		return errors.New("usage: pkgdepot remove [options] <repository> <architecture> <package-name>")
	}
	client := httpclient.New(cmd.String("url"), cmd.String("token"))
	return client.Remove(ctx, cmd.Args().Get(0), cmd.Args().Get(1), cmd.Args().Get(2))
}
