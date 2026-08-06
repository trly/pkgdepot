package main

import (
	"github.com/trly/pkgdepot/internal/command"
	"github.com/trly/pkgdepot/internal/repository"
	"github.com/urfave/cli/v3"
)

func repoCommand() *cli.Command {
	return &cli.Command{
		Name:  "repo",
		Usage: "manage local package repositories",
		Commands: []*cli.Command{
			repoListCommand(),
			repoRemoveCommand(),
			repoRenameCommand(),
			repoCreateCommand(),
		},
	}
}

func localRepositories(cmd *cli.Command) (*repository.Service, error) {
	repositories := repository.New(cmd.String("data-root"), command.NewPacman())
	if err := repositories.Initialize(); err != nil {
		return nil, err
	}
	return repositories, nil
}
