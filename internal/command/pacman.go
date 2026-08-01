package command

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type RepositoryCommands interface {
	Add(context.Context, string, string) error
	Remove(context.Context, string, string) error
}

type Pacman struct {
	RepoAdd    string
	RepoRemove string
}

func NewPacman() Pacman {
	return Pacman{RepoAdd: "/usr/bin/repo-add", RepoRemove: "/usr/bin/repo-remove"}
}

func (p Pacman) Add(ctx context.Context, database, packagePath string) error {
	return run(ctx, p.RepoAdd, "--include-sigs", "--wait-for-lock", database, packagePath)
}

func (p Pacman) Remove(ctx context.Context, database, packageName string) error {
	return run(ctx, p.RepoRemove, "--wait-for-lock", database, packageName)
}

func run(ctx context.Context, executable string, arguments ...string) error {
	output, err := exec.CommandContext(ctx, executable, arguments...).CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}
		return fmt.Errorf("%s: %s", executable, message)
	}
	return nil
}
