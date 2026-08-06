package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/trly/pkgdepot/internal/token"
	"github.com/urfave/cli/v3"
)

func tokenCreateCommand() *cli.Command {
	return &cli.Command{
		Name:      "create",
		Usage:     "create a local API credential",
		ArgsUsage: "<name>",
		Flags: append(tokenStoreFlags(),
			&cli.StringSliceFlag{Name: "permission", Aliases: []string{"p"}, Usage: "permission: package:publish or package:remove", Required: true},
			&cli.StringFlag{Name: "repository", Usage: "restrict to a repository"},
			&cli.StringFlag{Name: "architecture", Usage: "restrict to an architecture"},
			&cli.StringFlag{Name: "expires-at", Usage: "RFC 3339 expiry timestamp"},
		),
		Action: tokenCreate,
	}
}

func tokenCreate(_ context.Context, cmd *cli.Command) error {
	if cmd.NArg() != 1 {
		return errors.New("usage: pkgdepot token create [options] <name>")
	}
	expiresAt, err := parseExpiry(cmd.String("expires-at"))
	if err != nil {
		return err
	}
	store, err := localTokenStore(cmd)
	if err != nil {
		return err
	}
	info, credential, err := store.Create(token.CreateOptions{
		Name:         cmd.Args().First(),
		Permissions:  cmd.StringSlice("permission"),
		Repository:   cmd.String("repository"),
		Architecture: cmd.String("architecture"),
		ExpiresAt:    expiresAt,
	})
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		token.Info
		Credential string `json:"credential"`
	}{Info: info, Credential: credential})
}
