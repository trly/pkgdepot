package main

import (
	"context"
	"errors"

	"github.com/trly/pkgdepot/internal/command"
	"github.com/trly/pkgdepot/internal/config"
	"github.com/trly/pkgdepot/internal/repository"
	"github.com/urfave/cli/v3"
)

func renameCommand() *cli.Command {
	return &cli.Command{
		Name:        "rename",
		Usage:       "rename a local package repository",
		ArgsUsage:   "<old-repository> <new-repository>",
		Description: "Create a renamed snapshot of a repository and its architecture databases in the local data root.",
		Flags: []cli.Flag{&cli.StringFlag{
			Name:    "data-root",
			Usage:   "pkgdepot data root",
			Value:   config.DefaultDataRoot,
			Sources: cli.EnvVars("PKGDEPOT_DATA_ROOT"),
		}},
		Action: renameRepository,
	}
}

func renameRepository(_ context.Context, cmd *cli.Command) error {
	if cmd.NArg() != 2 {
		return errors.New("usage: pkgdepot rename [options] <old-repository> <new-repository>")
	}
	repositories := repository.New(cmd.String("data-root"), command.NewPacman())
	if err := repositories.Initialize(); err != nil {
		return err
	}
	return repositories.Rename(cmd.Args().Get(0), cmd.Args().Get(1))
}
