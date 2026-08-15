package main

import "github.com/urfave/cli/v3"

func clientFlags() []cli.Flag {
	return []cli.Flag{
		urlFlag(),
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
		Value:   "http://127.0.0.1:8080",
		Sources: cli.EnvVars("PKGDEPOT_URL"),
	}
}
