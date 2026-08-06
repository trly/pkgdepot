package main

import (
	"context"
	"errors"

	"github.com/urfave/cli/v3"
)

func repoCreateCommand() *cli.Command {
	return &cli.Command{
		Name: "create", Usage: "create a local package repository", ArgsUsage: "<repository>",
		Flags: tokenStoreFlags(), Action: repoCreate,
	}
}

func repoCreate(_ context.Context, cmd *cli.Command) error {
	if cmd.NArg() != 1 {
		return errors.New("usage: pkgdepot repo create [options] <repository>")
	}
	repositories, err := localRepositories(cmd)
	if err != nil {
		return err
	}
	return repositories.Create(cmd.Args().First())
}
