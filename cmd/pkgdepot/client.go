package main

import "github.com/urfave/cli/v3"

func clientFlags() []cli.Flag {
	return []cli.Flag{
		urlFlag(),
		&cli.StringFlag{
			Name:    "token",
			Usage:   "locally generated API credential",
			Sources: cli.EnvVars("PKGDEPOT_CREDENTIAL"),
		},
	}
}

func architectureFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:  "architecture",
		Usage: "target architecture",
		Value: "x86_64",
	}
}

func urlFlag() *cli.StringFlag {
	return &cli.StringFlag{
		Name:    "url",
		Usage:   "pkgdepot server URL",
		Value:   "http://localhost:8080",
		Sources: cli.EnvVars("PKGDEPOT_URL"),
	}
}
