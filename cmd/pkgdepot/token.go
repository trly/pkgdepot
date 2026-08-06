package main

import (
	"errors"
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
