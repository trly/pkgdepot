package main

import (
	"context"
	"errors"

	"github.com/trly/pkgdepot/internal/httpclient"
	"github.com/urfave/cli/v3"
)

func repoRemoveCommand() *cli.Command {
	return &cli.Command{
		Name: "remove", Usage: "remove a package repository", ArgsUsage: "<repository>",
		Flags: clientFlags(), Action: repoRemove,
	}
}

func repoRemove(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() != 1 {
		return errors.New("usage: pkgdepot repo remove [options] <repository>")
	}
	client := httpclient.New(ctx, cmd.String("url"))
	return client.RemoveRepository(ctx, cmd.Args().First())
}
