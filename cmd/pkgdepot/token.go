package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/trly/pkgdepot/internal/config"
	"github.com/trly/pkgdepot/internal/token"
	"github.com/urfave/cli/v3"
)

func tokenCommand() *cli.Command {
	return &cli.Command{
		Name:  "token",
		Usage: "manage local API credentials",
		Commands: []*cli.Command{
			tokenCreateCommand(),
			tokenListCommand(),
			tokenRevokeCommand(),
			tokenRotateCommand(),
		},
	}
}

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

func tokenListCommand() *cli.Command {
	return &cli.Command{Name: "list", Usage: "list local API credentials", Flags: tokenStoreFlags(), Action: tokenList}
}

func tokenRevokeCommand() *cli.Command {
	return &cli.Command{Name: "revoke", Usage: "revoke a local API credential", ArgsUsage: "<id>", Flags: tokenStoreFlags(), Action: tokenRevoke}
}

func tokenRotateCommand() *cli.Command {
	return &cli.Command{Name: "rotate", Usage: "rotate a local API credential", ArgsUsage: "<id>", Flags: tokenStoreFlags(), Action: tokenRotate}
}

func tokenStoreFlags() []cli.Flag {
	return []cli.Flag{&cli.StringFlag{
		Name:    "data-root",
		Usage:   "pkgdepot data root",
		Value:   config.DefaultDataRoot,
		Sources: cli.EnvVars("PKGDEPOT_DATA_ROOT"),
	}}
}

func localTokenStore(cmd *cli.Command) (*token.Store, error) {
	store := token.New(cmd.String("data-root"))
	if err := store.Initialize(); err != nil {
		return nil, err
	}
	return store, nil
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

func tokenList(_ context.Context, cmd *cli.Command) error {
	store, err := localTokenStore(cmd)
	if err != nil {
		return err
	}
	infos, err := store.List()
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(infos)
}

func tokenRevoke(_ context.Context, cmd *cli.Command) error {
	if cmd.NArg() != 1 {
		return errors.New("usage: pkgdepot token revoke [options] <id>")
	}
	store, err := localTokenStore(cmd)
	if err != nil {
		return err
	}
	return store.Revoke(cmd.Args().First())
}

func tokenRotate(_ context.Context, cmd *cli.Command) error {
	if cmd.NArg() != 1 {
		return errors.New("usage: pkgdepot token rotate [options] <id>")
	}
	store, err := localTokenStore(cmd)
	if err != nil {
		return err
	}
	info, credential, err := store.Rotate(cmd.Args().First())
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(struct {
		token.Info
		Credential string `json:"credential"`
	}{Info: info, Credential: credential})
}

func parseExpiry(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	expiresAt, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, errors.New("expiry must be an RFC 3339 timestamp")
	}
	return expiresAt, nil
}
