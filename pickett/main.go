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

func main() {
	var debug bool
	var showDocker bool
	var configFile string

	flag.BoolVar(&debug, "debug", false, "turns off verbose logging for pickett developers")
	flag.BoolVar(&showDocker, "showdocker", false, "turns on tracing of docker commands issued")
	flag.StringVar(&configFile, "config", "Pickett.json", "use a custom pickett configuration file")
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		usage()
	}

	wd, err := os.Getwd()
	if err != nil {
		panic("cant get working directory!")
	}

	helper, docker, etcd, vbox := makeIOObjects(debug, showDocker, filepath.Join(wd, configFile))
	reader := helper.ConfigReader()
	config, err := pickett.NewConfig(reader, helper, docker, etcd, vbox)
	helper.CheckFatal(err, "can't understand config file %s: %v", helper.ConfigFile())

	switch args[0] {
	case "run":
		pickett.CmdRun(args[1:], config)
	case "status":
		pickett.CmdStatus(config)
	case "stop":
		pickett.CmdStop(args[1:], config)
	case "drop":
		pickett.CmdDrop(args[1:], config)
	case "wipe":
		pickett.CmdWipe(args[1:], config)
	default:
		usage()
	}

	os.Exit(0)
}

func usage() {
	// There doesn't seem to be a better way to mix flags usage with arguments usage ?
	error := fmt.Errorf(`Usage of pickett, expected an action as the first argument, one of:
		- run [tags]      Runs all or a a specific tagged target(s). 
		- status          Shows the status of all the known tagged targets. 
		- stop [tags]     Stop all or a specific tagged target(s). 
		- drop [tags]     Stop and delete all or a specific tagged target(s). 
		- wipe [tags]     Stop and delete all or a specific tagged target(s) container(s) and images(s).`)
	fmt.Print(error)
	os.Exit(1)
}
