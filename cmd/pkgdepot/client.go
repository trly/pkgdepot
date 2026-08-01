package main

import "github.com/urfave/cli/v3"

func clientFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "url",
			Usage:   "pkgdepot server URL",
			Value:   "http://localhost:8080",
			Sources: cli.EnvVars("PKGDEPOT_URL"),
		},
		&cli.StringFlag{
			Name:    "token",
			Usage:   "management bearer token",
			Sources: cli.EnvVars("PKGDEPOT_TOKEN"),
		},
	}
}
