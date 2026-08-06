package main

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"
)

func main() {
	app := &cli.Command{
		Name:  "pkgdepot",
		Usage: "manage an Arch Linux package repository",
		Commands: []*cli.Command{
			serveCommand(),
			tokenCommand(),
			packageCommand(),
			repoCommand(),
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "pkgdepot:", err)
		os.Exit(1)
	}
}
