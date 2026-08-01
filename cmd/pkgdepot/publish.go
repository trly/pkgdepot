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
		Usage:     "publish a package",
		ArgsUsage: "<repository> <architecture> <package>",
		Flags: append(clientFlags(), &cli.StringFlag{
			Name:  "signature",
			Usage: "detached package signature",
		}),
		Action: publish,
	}
}

func publish(ctx context.Context, cmd *cli.Command) error {
	if cmd.NArg() != 3 {
		return errors.New("usage: pkgdepot publish [options] <repository> <architecture> <package>")
	}
	client := httpclient.New(cmd.String("url"), cmd.String("token"))
	pkg, err := client.Publish(ctx, cmd.Args().Get(0), cmd.Args().Get(1), cmd.Args().Get(2), cmd.String("signature"))
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(pkg)
}
