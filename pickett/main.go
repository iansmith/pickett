package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/igneous-systems/pickett"
	"github.com/igneous-systems/pickett/io"
)

func contains(s []string, target string) bool {
	for _, candidate := range s {
		if candidate == target {
			return true
		}
	}
	return false
}

func makeIOObjects(debug, showDocker bool, path string) (io.Helper, io.DockerCli, io.EtcdClient, io.VirtualBox) {
	helper, err := io.NewHelper(path, debug)
	if err != nil {
		//no helper, so can't call CheckFatal()
		fmt.Fprintf(os.Stderr, "[pickett] can't read %s: %v\n", path, err)
		os.Exit(1)
	}
	cli, err := io.NewDockerCli(debug, showDocker)
	helper.CheckFatal(err, "failed to connect to docker server, maybe its not running? %v")
	etcd, err := io.NewEtcdClient(debug)
	helper.CheckFatal(err, "failed to connect to etcd, maybe its not running? %v")
	vbox, err := io.NewVirtualBox(debug)
	helper.CheckFatal(err, "failed to run vboxmanage: %v")
	return helper, cli, etcd, vbox
}

// trueMain is the entry point of the program with the targets filled in
// and a working helper.
func trueMain(targets []string, helper io.Helper, cli io.DockerCli, etcd io.EtcdClient, vbox io.VirtualBox) {
	reader := helper.ConfigReader()
	config, err := pickett.NewConfig(reader, helper, cli)
	helper.CheckFatal(err, "can't understand config file %s: %v", helper.ConfigFile())
	buildables, runnables := config.EntryPoints()

	run := false
	runTarget := ""

	// if you don't tell us what to build, we build everything with no outgoing
	// edges, the "root" of a backchain
	if len(targets) == 0 {
		targets = buildables
	} else {
		//if you do tell us, we need know if it's runnable
		for _, t := range targets {
			if contains(buildables, t) {
				continue
			}
			if contains(runnables, t) {
				if run {
					fmt.Fprintf(os.Stderr, "[pickett] can only run one target (%s and %s both runnable)\n", runTarget, t)
					os.Exit(1)
				}
				run = true
				runTarget = t
				continue
			}
			fmt.Fprintf(os.Stderr, "[pickett] don't know anything about target %s\n", t)
			os.Exit(1)
		}
	}
	for _, target := range targets {
		if target == runTarget {
			continue
		}
		err := config.Build(target, helper, cli, etcd, vbox)
		helper.CheckFatal(err, "%s: %v", target)
	}
	if runTarget != "" {
		err := config.Build(runTarget, helper, cli, etcd, vbox)
		helper.CheckFatal(err, "%s: %v", runTarget)
		err = config.Execute(runTarget, helper, cli, etcd, vbox)
		helper.CheckFatal(err, "%s: %v", runTarget)
	}
}

func main() {
	var debug bool
	var showDocker bool
	var configFile string

	flag.BoolVar(&debug, "debug", false, "turns off verbose logging for pickett developers")
	flag.BoolVar(&showDocker, "showdocker", false, "turns on tracing of docker commands issued")
	flag.StringVar(&configFile, "config", "Pickett.json", "use a custom pickett configuration file")
	flag.Parse()

	wd, err := os.Getwd()
	if err != nil {
		panic("cant get working directory!")
	}

	helper, docker, etcd, vbox := makeIOObjects(debug, showDocker, filepath.Join(wd, configFile))
	trueMain(flag.Args(), helper, docker, etcd, vbox)
	os.Exit(0)
}
