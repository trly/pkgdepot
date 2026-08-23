package main

import (
	"context"
	"fmt"
	"os"

	"github.com/trly/pkgdepot/internal/httpclient"
	"github.com/urfave/cli/v3"
)

var defaultOAuthScopes = []string{}

func loginCommand() *cli.Command {
	return &cli.Command{
		Name:  "login",
		Usage: "authenticate with the pkgdepot server",
		Flags: append(clientFlags(), &cli.StringSliceFlag{
			Name:  "scope",
			Usage: "OAuth scope to request; may be repeated",
			Value: append([]string(nil), defaultOAuthScopes...),
		}),
		Action: login,
	}
}

func login(ctx context.Context, cmd *cli.Command) error {
	client := httpclient.New(ctx, cmd.String("url"))
	token, err := client.Login(ctx, cmd.StringSlice("scope"))
	if err != nil {
		return err
	}
	if token.Expiry.IsZero() {
		fmt.Fprintln(os.Stderr, "Authenticated; token has no reported expiry.")
		return nil
	}
	fmt.Fprintf(os.Stderr, "Authenticated; token cached until %s.\n", token.Expiry.UTC().Format("2006-01-02 15:04:05 MST"))
	return nil
}
