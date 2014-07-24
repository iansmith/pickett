package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/igneous-systems/pickett"
	"github.com/igneous-systems/pickett/io"
)

func makeIOObjects(debug bool, path string) (io.Helper, io.DockerCli, io.EtcdClient, io.VirtualBox) {
	helper, err := io.NewHelper(path, debug)
	if err != nil {
		//no helper, so can't call CheckFatal()
		fmt.Fprintf(os.Stderr, "[pickett] can't read %s: %v\n", path, err)
		os.Exit(1)
	}
	cli, err := io.NewDockerCli(debug)
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

	// if you don't tell us what to build, we build everything with no outgoing
	// edges, the "root" of a backchain
	if len(targets) == 0 {
		targets = config.Sinks()
	}
	for _, target := range targets {
		err := config.Initiate(target, helper, cli, etcd, vbox)
		helper.CheckFatal(err, "%s: %v", target)
	}
}

func main() {
	var debug bool
	var configFile string

	flag.BoolVar(&debug, "debug", false, "turns off verbose logging for pickett developers")
	flag.StringVar(&configFile, "config", "Pickett.json", "use a custom pickett configuration file")
	flag.Parse()

	wd, err := os.Getwd()
	if err != nil {
		panic("cant get working directory!")
	}

	helper, docker, etcd, vbox := makeIOObjects(debug, filepath.Join(wd, configFile))
	trueMain(flag.Args(), helper, docker, etcd, vbox)
	os.Exit(0)
}
