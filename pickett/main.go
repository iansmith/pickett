package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/igneous-systems/logit"
	"gopkg.in/alecthomas/kingpin.v1"

	"github.com/igneous-systems/pickett"
	"github.com/igneous-systems/pickett/io"
)

var (
	PickettVersion = "0.0.1"

	app = kingpin.New("Pickett", "Make for the docker world.")

	// Global flags
	debug      = app.Flag("debug", "Enable debug mode.").Short('d').Bool()
	configFile = app.Flag("configFile", "Config file.").Short('c').Default("Pickett.json").String()

	// Actions
	run     = app.Command("run", "Runs a specific node in a topology, including all depedencies.")
	runTopo = run.Arg("topo", "Topo node.").Required().String()

	status        = app.Command("status", "Shows the status of all the known buildable tags and/or runnable nodes.")
	statusTargets = status.Arg("targets", "Tags / Nodes").Strings()

	build     = app.Command("build", "Build all tags or specified tags.")
	buildTags = build.Arg("tags", "Tags").Strings()

	stop      = app.Command("stop", "Stop all or a specific node.")
	stopNodes = stop.Arg("topology.nodes", "Topology Nodes").Strings()

	drop      = app.Command("drop", "Stop and delete all or specific node.")
	dropNodes = drop.Arg("topology.nodes", "Topology Nodes").Strings()

	wipe     = app.Command("wipe", "Delete all or specified tag (force rebuild next time).")
	wipeTags = wipe.Arg("tags", "Tags").Strings()

	ps      = app.Command("ps", "Give 'docker ps' like output of running topologies.")
	psNodes = ps.Arg("topology.nodes", "Topology Nodes").Strings()

	inject     = app.Command("inject", "Run the given command in the given topology node")
	injectNode = inject.Arg("topology.node", "Topology Node").Required().String()
	injectCmd  = inject.Arg("Cmd", "Node").Required().Strings()
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
	os.Exit(wrappedMain())
}

// Wrapped to make os.Exit work well with logit
func wrappedMain() int {

	kingpin.Version(PickettVersion)

	action := kingpin.MustParse(app.Parse(os.Args[1:]))

	var logFilterLvl logit.Level
	if *debug {
		logFilterLvl = logit.DEBUG
	} else {
		logFilterLvl = logit.INFO
	}
	logit.Global.ModifyFilterLvl("stdout", logFilterLvl, nil, nil)
	defer logit.Flush(-1)

	wd, err := os.Getwd()
	if err != nil {
		panic("can't get working directory!")
	}

	if os.Getenv("DOCKER_HOST") == "" {
		fmt.Fprintf(os.Stderr, "DOCKER_HOST not set; suggest DOCKER_HOST=tcp://:2375 (for local launcher)\n")
		return 1
	}

	_, err = os.Open(filepath.Join(wd, *configFile))
	if err != nil {
		fmt.Fprintf(os.Stderr, "./%s not found (cwd: %s)\n", *configFile, wd)
		return 1
	}

	helper, docker, etcd, vbox, err := makeIOObjects(filepath.Join(wd, *configFile))
	if err != nil {
		flog.Errorf("%v", err)
		return 1
	}
	reader := helper.ConfigReader()
	config, err := pickett.NewConfig(reader, helper, docker, etcd, vbox)
	if err != nil {
		flog.Errorf("Can't understand config file %s: %v", err.Error(), helper.ConfigFile())
		return 1
	}

	switch action {
	case "run":
		err = pickett.CmdRun(*runTopo, config)
	case "build":
		err = pickett.CmdBuild(*buildTags, config)
	case "status":
		err = pickett.CmdStatus(*statusTargets, config)
	case "stop":
		err = pickett.CmdStop(*stopNodes, config)
	case "drop":
		err = pickett.CmdDrop(*dropNodes, config)
	case "wipe":
		err = pickett.CmdWipe(*wipeTags, config)
	case "ps":
		err = pickett.CmdPs(*psNodes, config)
	case "inject":
		err = pickett.CmdInject(*injectNode, *injectCmd, config)
	default:
		app.Usage(os.Stderr)
		return 1
	}

	if err != nil {
		flog.Errorf("%s: %v", action, err)
		return 1
	}
	return 0
}
