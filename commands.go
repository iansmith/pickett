package pickett

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/igneous-systems/pickett/io"
)

// CmdRun is the 'run' entry point of the program with the targets filled in
// and a working helper.
func CmdRun(targets []string, config *Config) error {
	_, runnables := config.EntryPoints()
	if len(targets) == 0 {
		return fmt.Errorf("must supply a target to run (one of %s)\n", strings.Trim(fmt.Sprint(runnables), "[]"))
	}
	if len(targets) > 1 {
		return fmt.Errorf("too many arguments to run--can only run one target at a time\n")
	}
	return config.Execute(targets[0])
}

//return value is a bit tricky here for the primary return.  If it's nil
//then the entire topology is not known.  If its an empty map, then node is
//not known but the topology is.  Otherwise, it's a map from integer instance
//numbers to container names. If the string value is empty, it means that we
//have seen this instance before but not available at the present time.
func statusInstances(topoName string, nodeName string, config *Config) (map[int]string, error) {
	topology, ok := config.nameToTopology[topoName]
	if !ok {
		return nil, fmt.Errorf("bad topology name: %s", topoName)
	}
	_, ok = topology[nodeName]
	if !ok {
		return nil, fmt.Errorf("bad topology entry: %s", nodeName)
	}

	contPath := filepath.Join(io.PICKETT_KEYSPACE, CONTAINERS)
	topos, err := config.etcd.Children(contPath)
	if err != nil {
		return nil, err
	}
	if !contains(topos, topoName) {
		return nil, nil
	}
	result := make(map[int]string)

	nodePath := filepath.Join(io.PICKETT_KEYSPACE, CONTAINERS, topoName)
	nodes, err := config.etcd.Children(nodePath)
	if err != nil {
		return nil, fmt.Errorf("%v, maybe you've never run anything before?", err)
	}
	if !contains(nodes, nodeName) {
		return result, nil
	}
	instPath := filepath.Join(io.PICKETT_KEYSPACE, CONTAINERS, topoName, nodeName)
	instances, err := config.etcd.Children(instPath)
	if err != nil {
		return nil, err
	}
	for _, inst := range instances {
		x, err := strconv.ParseInt(inst, 10, 32)
		if err != nil {
			return nil, err
		}
		i := int(x)
		cont, found, err := config.etcd.Get(filepath.Join(io.PICKETT_KEYSPACE, CONTAINERS, topoName, nodeName, inst))
		if err != nil {
			return nil, err
		}
		if found {
			if strings.HasPrefix(cont, "/") {
				cont = cont[1:]
			}
			result[i] = cont
		} else {
			result[i] = ""
		}
	}
	return result, nil
}

const TIME_FORMAT = "01/02/06-03:04PM"

// CmdBuild builds all the targets you supplied, or all the final
//results if you don't supply anything. This is the analogue of CmdRun.
func CmdBuild(targets []string, config *Config) error {
	buildables, _ := config.EntryPoints()
	toBuild := buildables
	if len(targets) > 0 {
		toBuild = []string{}
		for _, targ := range targets {
			if !contains(buildables, targ) {
				fmt.Errorf("%s is not buildable, ignoring", targ)
				continue
			}
			toBuild = append(toBuild, targ)
		}
	}
	for _, build := range toBuild {
		err := config.Build(build)
		if err != nil {
			return err
		}
	}
	return nil
}

func chosenRunnables(config *Config, targets []string) []string {
	_, runnables := config.EntryPoints()
	if len(targets) == 0 {
		return runnables
	}
	run := []string{}
	for _, targ := range targets {
		if contains(runnables, targ) {
			run = append(run, targ)
		}
	}
	return run
}

// CmdStatus shows the status of all known targets or the set you supply
func CmdStatus(targets []string, config *Config) error {
	runStatus := chosenRunnables(config, targets)
	all, _ := config.EntryPoints()
	buildStatus := all

	if len(targets) != 0 {
		buildStatus := []string{}
		for _, targ := range targets {
			if contains(all, targ) {
				buildStatus = append(buildStatus, targ)
			}
		}
	}
	for _, target := range buildStatus {
		insp, err := config.cli.InspectImage(target)
		if err != nil && err.Error() != "no such image" {
			return err
		}
		if err != nil {
			fmt.Printf("%-25s | %-31s\n", target, "not found")
		} else {
			fmt.Printf("%-25s | %-31s\n", target, insp.CreatedTime().Format(TIME_FORMAT))
		}
	}

	for _, target := range runStatus {
		pair := strings.Split(target, ".")
		if len(pair) != 2 {
			panic(fmt.Sprintf("can't understand the target %s", target))
		}
		instances, err := statusInstances(pair[0], pair[1], config)
		flog.Infof("XXX %s checked ... %d", target, len(instances))
		if err != nil {
			return err
		}
		for i, cont := range instances {
			extra := fmt.Sprintf("[%d]", i)
			insp, err := config.cli.InspectContainer(cont)
			if err != nil {
				fmt.Printf("container %s not inspected: %v\n", cont, err)
				continue
			}
			if insp.Running() {
				extra += "*"
			}
			fmt.Printf("%-25s | %-31s | %-19s\n", target+extra, cont, insp.CreatedTime().Format(TIME_FORMAT))
		}
	}
	return nil
}

// CmdStop stops the targets containers
func CmdStop(targets []string, config *Config) error {
	stopSet := chosenRunnables(config, targets)
	for _, stop := range stopSet {
		pair := strings.Split(stop, ".")
		if len(pair) != 2 {
			panic(fmt.Sprintf("can't understand the target %s", stop))
		}
		instances, err := statusInstances(pair[0], pair[1], config)
		if err != nil {
			return err
		}
		for _, contId := range instances {
			insp, err := config.cli.InspectContainer(contId)
			if err != nil {
				return err
			}
			if insp.Running() {
				fmt.Printf("[pickett] trying to stop %s [%s]\n", contId, stop)
				if err := config.cli.CmdStop(contId); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// CmdDrop stops and removes the targets containers
func CmdDrop(targets []string, config *Config) error {
	err := CmdStop(targets, config)
	if err != nil {
		return err
	}
	dropSet := chosenRunnables(config, targets)
	for _, drop := range dropSet {
		pair := strings.Split(drop, ".")
		if len(pair) != 2 {
			panic(fmt.Sprintf("can't understand the target %s", drop))
		}
		instances, err := statusInstances(pair[0], pair[1], config)
		if err != nil {
			return err
		}
		for i, contId := range instances {
			if err := config.cli.CmdRmContainer(contId); err != nil {
				return err
			}
			key := filepath.Join(io.PICKETT_KEYSPACE, CONTAINERS, pair[0], pair[1], fmt.Sprint(i))
			oldId, err := config.etcd.Del(key)
			if err != nil || oldId != contId {
				if err != nil {
					return err
				}
				return fmt.Errorf("Unexpected container id: expecting %s but got %s!", contId, oldId)
			}
		}
	}
	return nil
}

// CmdWipe stops the targets containers
func CmdWipe(targets []string, config *Config) error {
	buildables := []string{}
	for k, _ := range config.nameToNode {
		buildables = append(buildables, k)
	}
	toWipe := buildables
	if len(targets) > 0 {
		toWipe := []string{}
		for _, t := range targets {
			if !contains(buildables, t) {
				return fmt.Errorf("don't know anything about %s", t)
			}
			toWipe = append(toWipe, t)
		}
	}
	for _, image := range toWipe {
		err := config.cli.CmdRmImage(image)
		if err != nil {
			if err.Error() == "no such image" {
				continue
			}
			if strings.HasPrefix(err.Error(), "API error (409): Conflict") {
				fmt.Printf("[pickett] image %s is in use, ignoring\n", image)
				continue
			}
			return fmt.Errorf("%s: %v", image, err)
		}
	}
	return nil
}

// checkTargets check the targets against the targets found in the config,
// returns an error if it's not matching, nil otherwise
func checkTargets(config *Config, targets []string) error {
	confTargets := confTargets(config)
	for _, target := range targets {
		if !contains(confTargets, target) {
			return fmt.Errorf("Unknowm target %s", target)
		}
	}
	return nil
}

// allTargets returns all known target names
func confTargets(config *Config) []string {
	buildables, runnables := config.EntryPoints()
	all := append([]string{}, buildables...)
	all = append(all, runnables...)
	return all
}
