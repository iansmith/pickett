package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/igneous-systems/logit"
	"gopkg.in/alecthomas/kingpin.v1"

	"github.com/igneous-systems/pickett/core"
	"github.com/igneous-systems/pickett/io"
)

var (
	PickettVersion = "0.0.1"

	app = kingpin.New("Pickett", "Make for the docker world.")

	// Global flags
	debug      = app.Flag("debug", "Enable debug mode.").Short('d').Bool()
	configFile = app.Flag("configFile", "Config file.").Short('f').Default("Pickett.json").String()

	// Actions
	run         = app.Command("run", "Runs a specific node in a topology, including all depedencies.")
	runTopo     = run.Arg("topo", "Topo node.").Required().String()
	runRootName = run.Flag("rootname", "Root name (prefix) for containers").Default(os.Getenv("USER")).String()
	runVol      = run.Flag("runvol", "runvolume like /foo:/bar/foo").Short('r').String()

	status        = app.Command("status", "Shows the status of all the known buildable tags and/or runnable nodes.")
	statusTargets = status.Arg("targets", "Tags / Nodes").Strings()

	build     = app.Command("build", "Build all tags or specified tags.")
	buildTags = build.Arg("tags", "Tags").Strings()

	stop      = app.Command("stop", "Stop all or a specific node.")
	stopNodes = stop.Arg("topology.nodes", "Topology Nodes").Strings()

	drop         = app.Command("drop", "stop and delete specific containers based on topology")
	dropTopo     = drop.Arg("topo", "Topology Nodes").Required().String()
	dropRootName = drop.Flag("rootname", "Root name (prefix) for containers").Default(os.Getenv("USER")).String()

	wipe     = app.Command("wipe", "Delete all or specified tag (force rebuild next time).")
	wipeTags = wipe.Arg("tags", "Tags").Strings()

	ps      = app.Command("ps", "Give 'docker ps' like output of running topologies.")
	psNodes = ps.Arg("topology.nodes", "Topology Nodes").Strings()

	inject     = app.Command("inject", "Run the given command in the given topology node")
	injectNode = inject.Arg("topology.node", "Topology Node").Required().String()
	injectCmd  = inject.Arg("Cmd", "Node").Required().Strings()

	etcdGet    = app.Command("etcdget", "Get a value from Pickett's Etcd store.")
	etcdGetKey = etcdGet.Arg("key", "Etcd key (full path)").Required().String()
	etcdSet    = app.Command("etcdset", "Set a key/value pair in Pickett's Etcd store.")
	etcdSetKey = etcdSet.Arg("key", "Etcd key (full path)").Required().String()
	etcdSetVal = etcdSet.Arg("value", "Etcd value").Required().String()

	destroy = app.Command("destroy", "Remove all containers and images, wipe etcd")
)

func contains(s []string, target string) bool {
	for _, candidate := range s {
		if candidate == target {
			return true
		}
	}
	return false
}

func makeIOObjects(path string) (io.Helper, io.DockerCli, io.EtcdClient, error) {
	helper, err := io.NewHelper(path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("can't read %s: %v", path, err)
	}
	cli, err := io.NewDockerCli()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to connect to docker server, maybe its not running? %v", err)
	}
	etcd, err := io.NewEtcdClient()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to connect to etcd, maybe its not running? %v", err)
	}
	if err := cli.Ping(); err != nil {
		return nil, nil, nil, err
	}
	return helper, cli, etcd, nil
}

var flog = logit.NewNestedLoggerFromCaller(logit.Global)

func main() {
	os.Exit(wrappedMain())
}

func InitStackDumpOnSig1() {
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, syscall.SIGINT)
	go func() {
		<-sigc
		buf := make([]byte, 100000)
		n := runtime.Stack(buf, true)
		os.Stderr.Write(buf[0:n])
		os.Exit(1)
	}()
}

// Wrapped to make os.Exit work well with logit
func wrappedMain() int {

	kingpin.Version(PickettVersion)

	action := kingpin.MustParse(app.Parse(os.Args[1:]))

	// dump all goroutine stacks on ctrl-c
	InitStackDumpOnSig1()

	var logFilterLvl logit.Level
	if *debug {
		logFilterLvl = logit.DEBUG
	} else {
		logFilterLvl = logit.INFO
	}
	logit.Global.ModifyFilterLvl("stdout", logFilterLvl, nil, nil)
	defer logit.Flush(-1)

	if os.Getenv("DOCKER_HOST") == "" {
		fmt.Fprintf(os.Stderr, "DOCKER_HOST not set; suggest DOCKER_HOST=tcp://:2375 (for local launcher)\n")
		return 1
	}

	_, err := os.Open(*configFile)
	if err != nil {
		wd, _ := os.Getwd()
		fmt.Fprintf(os.Stderr, "%s not found (cwd: %s)\n", *configFile, wd)
		return 1
	}

	absconf, err := filepath.Abs(*configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}

	helper, docker, etcd, err := makeIOObjects(absconf)
	if err != nil {
		if strings.HasSuffix(err.Error(), "EOF") {
			flog.Warningf("We read an EOF from docker and that likely means that we can't reach the DOCKER_HOST")
		}
		flog.Errorf("%v (detail: %s)", err, err.Error())
		return 1
	}
	reader := helper.ConfigReader()
	config, err := pickett.NewConfig(reader, helper, docker, etcd)
	if err != nil {
		flog.Errorf("Can't understand config file %s: %v", err.Error(), helper.ConfigFile())
		return 1
	}

	returnCode := 0
	switch action {
	case "run":
		returnCode, err = pickett.CmdRun(*runRootName, *runTopo, *runVol, config)
	case "build":
		err = pickett.CmdBuild(*buildTags, config)
	case "status":
		err = pickett.CmdStatus(*statusTargets, config)
	case "stop":
		err = pickett.CmdStop(*stopNodes, config)
	case "drop":
		err = pickett.CmdDrop(*dropRootName, *dropTopo, config)
	case "wipe":
		err = pickett.CmdWipe(*wipeTags, config)
	case "ps":
		err = pickett.CmdPs(*psNodes, config)
	case "inject":
		err = pickett.CmdInject(*injectNode, *injectCmd, config)
	case "etcdget":
		val, _, err := etcd.Get(*etcdGetKey)
		if err != nil {
			fmt.Print(err)
			return 1
		}
		fmt.Print(val)
	case "etcdset":
		_, err := etcd.Put(*etcdSetKey, *etcdSetVal)
		if err != nil {
			fmt.Print(err)
			return 1
		}
	case "destroy":
		err = pickett.CmdDestroy(config)
	default:
		app.Usage(os.Stderr)
		return 1
	}

	if err != nil {
		flog.Errorf("%s: %v", action, err)
		return 1
	}
	return returnCode
}
