package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/trly/pkgdepot/internal/httpclient"
	"github.com/urfave/cli/v3"
)

func packagePublishCommand() *cli.Command {
	return &cli.Command{
		Name:      "publish",
		Usage:     "publish an Arch Linux package",
		ArgsUsage: "<repository> <package-path>",
		Description: `Upload a package to a repository and update its package database.

The repository is the repository name, architecture is the target architecture,
and package-path is the path to a local Arch Linux package archive. Use
--signature to upload a detached signature with the package.

Examples:
			pkgdepot package publish stable ./example-1.0-1-x86_64.pkg.tar.zst
	  pkgdepot package publish --architecture aarch64 stable ./example-1.0-1-aarch64.pkg.tar.zst
	  pkgdepot package publish --signature ./example-1.0-1-x86_64.pkg.tar.zst.sig stable ./example-1.0-1-x86_64.pkg.tar.zst`,
		Flags: append(append(clientFlags(), architectureFlag()), &cli.StringFlag{
			Name:  "signature",
			Usage: "path to the detached package signature",
		}),
		Action: publish,
	}
}

func publish(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() != 2 {
		return errors.New("usage: pkgdepot package publish [options] <repository> <package-path>")
	}
	client := httpclient.New(cmd.String("url"), cmd.String("token"))
	pkg, err := client.Publish(ctx, cmd.Args().First(), cmd.String("architecture"), cmd.Args().Get(1), cmd.String("signature"))
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(pkg)
}
