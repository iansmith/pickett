package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"igneous.io/pickett"
)

// trueMain is the entry point of the program with the targets filled in
// and a working helper.
func trueMain(targets []string, helper pickett.IOHelper) {
	reader := helper.ConfigReader()
	config, err := pickett.NewConfig(reader, helper)
	helper.CheckFatal(err, "can't understand config file %s: %v", helper.ConfigFile())

	// if you don't tell us what to build, we build everything with no outgoing
	// edges, the "root" of a backchain
	if len(targets) == 0 {
		targets = config.Sinks()
	}
	for _, target := range targets {
		err := config.Build(target, helper)
		helper.CheckFatal(err, "%s: %v", target)
	}
}

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "turns off verbose logging for pickett developers")
	flag.Parse()
	configFile := "Pickett.json"
	wd, err := os.Getwd()
	if err != nil {
		panic("cant get working directory!")
	}
	if flag.NArg() > 0 {
		configFile = flag.Arg(0)
	}
	rest := []string{}

	if flag.NArg() > 1 {
		rest = flag.Args()[1:]
	}

	helper, err := pickett.NewIOHelper(filepath.Join(wd, configFile), debug)
	if err != nil {
		//no helper, so can't call CheckFatal()
		fmt.Fprintf(os.Stderr, "[pickett] can't read %s: %v\n", configFile, err)
		os.Exit(1)
	}
	trueMain(rest, helper)
	os.Exit(0)
}
