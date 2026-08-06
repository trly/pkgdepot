package main

import (
	"context"
	"errors"

	"github.com/urfave/cli/v3"
)

func repoRemoveCommand() *cli.Command {
	return &cli.Command{
		Name: "remove", Usage: "remove a local package repository", ArgsUsage: "<repository>",
		Flags: tokenStoreFlags(), Action: repoRemove,
	}
}

func repoRemove(_ context.Context, cmd *cli.Command) error {
	if cmd.NArg() != 1 {
		return errors.New("usage: pkgdepot repo remove [options] <repository>")
	}
	repositories, err := localRepositories(cmd)
	if err != nil {
		return err
	}
	return repositories.RemoveRepository(cmd.Args().First())
}
