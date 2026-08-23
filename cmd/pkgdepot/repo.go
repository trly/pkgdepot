package main

import (
	"github.com/urfave/cli/v3"
)

func repoCommand() *cli.Command {
	return &cli.Command{
		Name:  "repo",
		Usage: "manage package repositories on a pkgdepot server",
		Commands: []*cli.Command{
			repoListCommand(),
			repoRemoveCommand(),
			repoRenameCommand(),
			repoCreateCommand(),
		},
	}
}
