package main

import (
	"context"
	"fmt"
	"os"

	"github.com/trly/pkgdepot/internal/httpclient"
	"github.com/urfave/cli/v3"
)

func logoutCommand() *cli.Command {
	return &cli.Command{
		Name:   "logout",
		Usage:  "remove the cached OAuth token",
		Flags:  clientFlags(),
		Action: logout,
	}
}

func logout(ctx context.Context, cmd *cli.Command) error {
	if err := httpclient.New(ctx, cmd.String("url")).Logout(); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "Logged out.")
	return nil
}
