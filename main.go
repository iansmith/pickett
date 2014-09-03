package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
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
	run          = app.Command("run", "Runs a specific node in a topology, including all depedencies.")
	runTopo      = run.Arg("topo", "Topo node.").Required().String()
	runVol       = run.Flag("runvol", "runvolume like /foo:/bar/foo").Short('r').String()
	runTopoName  = run.Arg("toponame", "topology name for creating containers").Default(os.Getenv("USER")).String()
	runNoCleanup = run.Flag("nocleanup", "dont reclaim containers that are started as part of run").Short('n').Bool()

	build     = app.Command("build", "Build all tags or specified tags.")
	buildTags = build.Arg("tags", "Tags").Strings()

	drop         = app.Command("drop", "Stop and delete running and stopped containers derived from a run")
	dropTopo     = drop.Arg("topo", "Topology Node").Required().String()
	dropTopoName = drop.Arg("toponame", "topology name for dropping containers").Default(os.Getenv("USER")).String()

	inject     = app.Command("inject", "Run the given command in the given topology node")
	injectNode = inject.Arg("topology.node", "Topology Node").Required().String()
	injectCmd  = inject.Arg("Cmd", "Node").Required().Strings()

	etcdGet    = app.Command("etcdget", "Get a value from Pickett's Etcd store.")
	etcdGetKey = etcdGet.Arg("key", "Etcd key (full path)").Required().String()
	etcdSet    = app.Command("etcdset", "Set a key/value pair in Pickett's Etcd store.")
	etcdSetKey = etcdSet.Arg("key", "Etcd key (full path)").Required().String()
	etcdSetVal = etcdSet.Arg("value", "Etcd value").Required().String()
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
	return helper, cli, etcd, nil
}

var flog = logit.NewNestedLoggerFromCaller(logit.Global)

func main() {
	os.Exit(wrappedMain())
}

// SigHandler listens for a given signal, and then evaluates pushed callbacks
// opposite the order in which they were pushed
// That is, it is a stack of calls to make when the signal arrives
type SigHandler interface {
	PushCallback(func())
	Close()
}

type sigHandler struct {
	sigc      chan os.Signal
	callbacks []func()
}

func NewSigHandler(sig ...os.Signal) SigHandler {
	s := new(sigHandler)
	s.sigc = make(chan os.Signal, 1)
	s.callbacks = make([]func(), 0)
	go s.listen()
	signal.Notify(s.sigc, sig...)
	return s
}

func (s *sigHandler) listen() {
	for {
		_, ok := <-s.sigc
		if ok {
			for i := len(s.callbacks) - 1; i >= 0; i-- {
				s.callbacks[i]()
			}
		} else {
			return
		}
	}
}

func (s *sigHandler) PushCallback(f func()) {
	s.callbacks = append(s.callbacks, f)
}

func (s *sigHandler) Close() {
	close(s.sigc)
}

// Wrapped to make os.Exit work well with logit
func wrappedMain() int {

	kingpin.Version(PickettVersion)

	action := kingpin.MustParse(app.Parse(os.Args[1:]))

	sigIntHandler := NewSigHandler(syscall.SIGINT, syscall.SIGTERM)
	defer sigIntHandler.Close()
	sigIntHandler.PushCallback(func() { os.Exit(1) })

	// if in debug mode, dump all goroutinstacks on ctrl-c
	if *debug {
		sigIntHandler.PushCallback(func() {
			buf := make([]byte, 100000)
			n := runtime.Stack(buf, true)
			os.Stderr.Write(buf[0:n])
		})
	}

	var logFilterLvl logit.Level
	if *debug {
		logFilterLvl = logit.DEBUG
	} else {
		logFilterLvl = logit.INFO
	}
	logit.Global.ModifyFilterLvl("stdout", logFilterLvl, nil, nil)
	sigIntHandler.PushCallback(func() { logit.Flush(-1) })
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
		flog.Errorf("%v", err)
		return 1
	}
	sigIntHandler.PushCallback(func() { docker.Cleanup() })
	defer docker.Cleanup()
	reader := helper.ConfigReader()
	config, err := pickett.NewConfig(reader, helper, docker, etcd)
	if err != nil {
		flog.Errorf("Can't understand config file %s: %v", err.Error(), helper.ConfigFile())
		return 1
	}

	returnCode := 0
	switch action {
	case "run":
		returnCode, err = pickett.CmdRun(*runTopoName, *runTopo, *runVol, *runNoCleanup, config)
	case "build":
		err = pickett.CmdBuild(*buildTags, config)
	case "drop":
		err = pickett.CmdDrop(*dropTopoName, *dropTopo, config)
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
