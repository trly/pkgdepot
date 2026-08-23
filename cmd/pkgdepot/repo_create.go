package main

import (
	"context"
	"errors"

	"github.com/trly/pkgdepot/internal/httpclient"
	"github.com/urfave/cli/v3"
)

func repoCreateCommand() *cli.Command {
	return &cli.Command{
		Name: "create", Usage: "create a package repository", ArgsUsage: "<repository>",
		Flags: clientFlags(), Action: repoCreate,
	}
}

func repoCreate(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() != 1 {
		return errors.New("usage: pkgdepot repo create [options] <repository>")
	}
	client := httpclient.New(ctx, cmd.String("url"))
	return client.CreateRepository(ctx, cmd.Args().First())
}
