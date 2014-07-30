package main

import (
	"fmt"
	"os"

	"github.com/igneous-systems/pickett/io"
)

func main() {
	cli, err := io.NewDockerCli(true, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error in creating abstraction: %v\n", err)
		os.Exit(1)
	}
	conf := &io.BuildConfig{}
	if err := cli.CmdBuild(conf,
		"/home/iansmith/samples.src/sample1/container/runner",
		"fleazil"); err != nil {
		fmt.Fprintf(os.Stderr, "error in build: %v\n", err)
		os.Exit(1)
	}
}
