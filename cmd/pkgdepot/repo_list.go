package main

import (
	"context"
	"encoding/json"
	"os"

	"github.com/trly/pkgdepot/internal/httpclient"
	"github.com/urfave/cli/v3"
)

func repoListCommand() *cli.Command {
	return &cli.Command{Name: "list", Usage: "list package repositories", Flags: clientFlags(), Action: repoList}
}

func repoList(ctx context.Context, cmd *cli.Command) error {
	client := httpclient.New(ctx, cmd.String("url"))
	items, err := client.Repositories(ctx)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(items)
}
