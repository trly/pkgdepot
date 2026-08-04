package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/trly/pkgdepot/internal/httpclient"
	"github.com/urfave/cli/v3"
)

func publishCommand() *cli.Command {
	return &cli.Command{
		Name:      "publish",
		Usage:     "publish an Arch Linux package",
		ArgsUsage: "<repository> <architecture> <package-path>",
		Description: `Upload a package to a repository and update its package database.

The repository is the repository name, architecture is the target architecture,
and package-path is the path to a local Arch Linux package archive. Use
--signature to upload a detached signature with the package.

Examples:
  pkgdepot publish stable x86_64 ./example-1.0-1-x86_64.pkg.tar.zst
  pkgdepot publish --signature ./example-1.0-1-x86_64.pkg.tar.zst.sig stable x86_64 ./example-1.0-1-x86_64.pkg.tar.zst`,
		Flags: append(clientFlags(), &cli.StringFlag{
			Name:  "signature",
			Usage: "path to the detached package signature",
		}),
		Action: publish,
	}
}

func publish(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() != 3 {
		return errors.New("usage: pkgdepot publish [options] <repository> <architecture> <package-path>")
	}
	client := httpclient.New(cmd.String("url"), cmd.String("token"))
	pkg, err := client.Publish(ctx, cmd.Args().Get(0), cmd.Args().Get(1), cmd.Args().Get(2), cmd.String("signature"))
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(pkg)
}
