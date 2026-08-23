package main

import (
	"context"
	"errors"

	"github.com/trly/pkgdepot/internal/httpclient"
	"github.com/urfave/cli/v3"
)

func repoRenameCommand() *cli.Command {
	return &cli.Command{
		Name:        "rename",
		Usage:       "rename a package repository",
		ArgsUsage:   "<old-repository> <new-repository>",
		Description: "Rename a repository and its architecture databases on the server.",
		Flags:       clientFlags(),
		Action:      renameRepository,
	}
}

func renameRepository(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() != 2 {
		return errors.New("usage: pkgdepot repo rename [options] <old-repository> <new-repository>")
	}
	client := httpclient.New(ctx, cmd.String("url"))
	return client.RenameRepository(ctx, cmd.Args().Get(0), cmd.Args().Get(1))
}
