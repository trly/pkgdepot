package main

import "github.com/urfave/cli/v3"

func packageCommand() *cli.Command {
	return &cli.Command{
		Name:  "package",
		Usage: "manage repository packages",
		Commands: []*cli.Command{
			packagePublishCommand(),
			packageRemoveCommand(),
			packageListCommand(),
		},
	}
}
