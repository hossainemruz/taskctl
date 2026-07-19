package main

import (
	"context"
	"os"

	"github.com/hossainemruz/taskctl/internal/cli"
)

var version = "dev"

func main() {
	dependencies := cli.DefaultDependencies()
	dependencies.Version = version
	os.Exit(cli.Execute(context.Background(), dependencies, os.Args[1:]))
}
