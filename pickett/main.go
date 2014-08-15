package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/igneous-systems/logit"
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

func makeIOObjects(path string) (io.Helper, io.DockerCli, io.EtcdClient, io.VirtualBox, error) {
	helper, err := io.NewHelper(path)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("can't read %s: %v", path, err)
	}
	cli, err := io.NewDockerCli()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to connect to docker server, maybe its not running? %v", err)
	}
	etcd, err := io.NewEtcdClient()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to connect to etcd, maybe its not running? %v", err)
	}
	vbox, err := io.NewVirtualBox()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("failed to run vboxmanage: %v", err)
	}
	return helper, cli, etcd, vbox, nil
}

// trueMain is the entry point of the program with the targets filled in
// and a working helper.
func trueMain(targets []string, helper io.Helper, cli io.DockerCli, etcd io.EtcdClient, vbox io.VirtualBox) error {
	reader := helper.ConfigReader()
	config, err := pickett.NewConfig(reader, helper, cli, etcd, vbox)
	if err != nil {
		return fmt.Errorf("can't understand config file %s: %v", helper.ConfigFile(), err)
	}
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
					return fmt.Errorf("can only run one target (%s and %s both runnable)", runTarget, t)
				}
				run = true
				runTarget = t
				continue
			}
			return fmt.Errorf("don't know anything about target %s", t)
		}
	}
	for _, target := range targets {
		if target == runTarget {
			continue
		}
		err := config.Build(target)
		if err != nil {
			return fmt.Errorf("an error occurred while building target '%v': %v", target, err)
		}
	}
	if runTarget != "" {
		err = config.Execute(runTarget)
		if err != nil {
			return fmt.Errorf("an error occurred while running target '%v': %v", runTarget, err)
		}
	}
	return nil
}

var flog = logit.NewNestedLoggerFromCaller(logit.Global)

func main() {
	var debug bool
	var configFile string

	flag.BoolVar(&debug, "debug", false, "turns on verbose logging for pickett developers")
	flag.StringVar(&configFile, "config", "Pickett.json", "use a custom pickett configuration file")
	flag.Parse()

	var logFilterLvl logit.Level
	if debug {
		logFilterLvl = logit.DEBUG
	} else {
		logFilterLvl = logit.INFO
	}
	logit.Global.ModifyFilterLvl("stdout", logFilterLvl, nil, nil)

	wd, err := os.Getwd()
	if err != nil {
		panic("can't get working directory!")
	}

	helper, docker, etcd, vbox, err := makeIOObjects(filepath.Join(wd, configFile))
	if err != nil {
		flog.Errorf("failed to make IO objects: %v", err)
	} else {
		err = trueMain(flag.Args(), helper, docker, etcd, vbox)
		if err != nil {
			flog.Errorln(err)
		}
	}

	logit.Flush(time.Millisecond * 300)
	if err != nil {
		os.Exit(1)
	} else {
		os.Exit(0)
	}
}
