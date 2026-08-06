package main

import (
	"context"
	"encoding/json"
	"os"

	"github.com/urfave/cli/v3"
)

func repoListCommand() *cli.Command {
	return &cli.Command{Name: "list", Usage: "list local package repositories", Flags: tokenStoreFlags(), Action: repoList}
}

func repoList(_ context.Context, cmd *cli.Command) error {
	repositories, err := localRepositories(cmd)
	if err != nil {
		return err
	}
	items, err := repositories.Repositories()
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(items)
}
