package pickett

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/igneous-systems/pickett/io"
)

type runVolumeSpec struct {
	source  string
	mountAt string
}

// CmdRun is the 'run' entry point of the program with the targets filled in
// and a working helper.
func CmdRun(rootName string, target string, runVol string, config *Config) (int, error) {
	var vol *runVolumeSpec
	if runVol != "" {
		pair := strings.Split(runVol, ":")
		if len(pair) != 2 {
			return 1, fmt.Errorf("unable to understand run volume (%s), should be /foo:/bar/foo", runVol)
		}
		vol = &runVolumeSpec{pair[0], pair[1]}
	}
	return config.Execute(rootName, target, vol)
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
	topos, found, err := config.etcd.Children(contPath)
	if !found {
		return nil, nil //nothing found at this level
	}
	if err != nil {
		return nil, err
	}
	if !contains(topos, topoName) {
		return nil, nil
	}
	result := make(map[int]string)

	nodePath := filepath.Join(io.PICKETT_KEYSPACE, CONTAINERS, topoName)
	nodes, found, err := config.etcd.Children(nodePath)
	if !found {
		return result, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%v, maybe you've never run anything before?", err)
	}
	if !contains(nodes, nodeName) {
		return result, nil
	}
	instPath := filepath.Join(io.PICKETT_KEYSPACE, CONTAINERS, topoName, nodeName)
	instances, found, err := config.etcd.Children(instPath)
	if !found {
		return result, nil
	}
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
				flog.Errorf("%s is not buildable, ignoring", targ)
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
			} else {
				flog.Errorf("unknown target %s (should be one of %s)", targ, all)
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
		if err != nil {
			return err
		}
		if len(instances) == 0 {
			fmt.Printf("%-25s | %-31s\n", target, "not found")
			continue
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
				flog.Errorf("Failed to inspect %s, already destroyed ? - %s", contId, err)
				continue // This can happen, so we should not error out.
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

func validateTopoName(target string, config *Config) (topoMap, *topoInfo, error) {
	pair := strings.Split(strings.Trim(target, " \n"), ".")
	if len(pair) != 2 {
		return nil, nil, fmt.Errorf("unable to understand '%s', expect something like 'foo.bar'", target)
	}
	tmap, isPresent := config.nameToTopology[pair[0]]
	if !isPresent {
		return nil, nil, fmt.Errorf("no such target for run: '%s'", pair[0])
	}
	var info *topoInfo
	for key, value := range tmap {
		if pair[1] == key {
			info = value
			break
		}
	}
	if info == nil {
		return nil, nil, fmt.Errorf("unable to understand '%s', expected something like foo.bar (%s is ok)", pair[1], pair[0])
	}
	return tmap, info, nil
}

// CmdDrop stops and removes the targets containers
func CmdDrop(rootName string, target string, config *Config) error {
	tmap, info, err := validateTopoName(target, config)
	if err != nil {
		return err
	}
	queue := []*topoInfo{info}
	names := []*io.StructuredContainerName{}

	for len(queue) > 0 {
		elem := queue[0]
		queue = queue[1:]

		for i := 0; i < elem.instances; i++ {
			scn := io.NewStructuredContainerName(rootName, elem.runner.name(), i)
			names = append(names, scn)
		outer:
			for _, c := range elem.runner.consumed() {
				for _, q := range queue {
					if q.runner.name() == c.name() {
						continue outer
					}
				}
				queue = append(queue, tmap[c.name()])
			}

		}
	}
	for _, scn := range names {
		name := scn.String()
		insp, err := config.cli.InspectContainer(name)

		if err != nil {
			if err.Error() != "No such container: "+name {
				return err
			}
			continue
		}
		if insp.Running() {
			if err := config.cli.CmdStop(name); err != nil {
				return err
			}
		}
		if err := config.cli.CmdRmContainer(name); err != nil {
			flog.Infof("HACK HACK on RM container:%s", err.Error())
			return err
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

func CmdPs(targets []string, config *Config) error {
	selected := chosenRunnables(config, targets)
	w := tabwriter.NewWriter(os.Stdout, 20, 1, 3, ' ', 0)
	fmt.Fprint(w, "TARGET\tNAME\tCONTAINER ID\tIP\tPorts\n")
	for _, target := range selected {
		pair := strings.Split(target, ".")
		if len(pair) != 2 {
			panic(fmt.Sprintf("can't understand the target %s", target))
		}

		instances, err := statusInstances(pair[0], pair[1], config)
		if err != nil {
			return err
		}

		for i, contId := range instances {
			insp, err := config.cli.InspectContainer(contId)
			if err != nil {
				return err
			}

			fmt.Fprintf(w, "%s.%v\t%s\t%s\t%s\t%v\n", target, i, insp.ContainerName(), insp.ContainerID()[:12],
				insp.Ip(), insp.Ports())
		}
	}
	w.Flush()
	return nil
}

func CmdInject(target string, cmds []string, config *Config) error {

	breakout := strings.Replace(target, ".", "/", -1)
	// NOTE TO SELF: write a tree-ish function that returns an enumeration/array of topo nodes
	cont, found, err := config.etcd.Get(filepath.Join(io.PICKETT_KEYSPACE, CONTAINERS, breakout))
	if err != nil {
		return err
	} else if !found {
		return fmt.Errorf("No instance information found in etcd, is `%v' running?", target)
	}

	strings.TrimPrefix(cont, "/")

	fmt.Printf("Inspecting %v\n", cont)
	insp, err := config.cli.InspectContainer(cont)
	if err != nil {
		return err
	}

	sudo := fmt.Sprintf("sudo sh -c 'cd /var/lib/docker/execdriver/native/%s && nsinit exec %s'",
		insp.ContainerID(), strings.Join(cmds, " "))
	cmd := exec.Command("vagrant", "ssh", "launcher", "-c", sudo)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Printf("==> launcher:  %v\n", sudo)
	return cmd.Run()
}

// CmdEtcdGet is used to retrieve a value from Etcd, given it's full key path
func CmdEtcdGet(key string, config *Config) error {
	val, found, err := config.etcd.Get(key)
	if found && err != nil {
		fmt.Println(val)
	}
	return err
}

// CmdEtcdPut is used to store a value in Etcd at the given it's full key path
func CmdEtcdPut(key string, val string, config *Config) error {
	_, err := config.etcd.Put(key, val)
	return err
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

// CmdDestroy stops and removes all containers, and removes all images
func CmdDestroy(config *Config) error {
	const Up = "Up"

	fmt.Println("clearing etcd")

	resps, found, err := config.etcd.Children("/")
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("Error: could not find '/' in etcd")
	}

	for _, resp := range resps {
		_, err := config.etcd.RecursiveDel("/" + resp)
		if err != nil {
			return err
		}
	}

	fmt.Println("stopping running containers")

	containers, err := config.cli.ListContainers()
	if err != nil {
		return err
	}

	for _, container := range containers {
		status := strings.Split(container.Status, " ")
		if status[0] == Up {
			err = config.cli.CmdStop(container.ID)
			if err != nil {
				return err
			}
		}
	}

	fmt.Println("removing containers")

	for _, container := range containers {
		err = config.cli.CmdRmContainer(container.ID)
		if err != nil {
			return err
		}
	}

	fmt.Println("removing images")

	images, err := config.cli.ListImages()
	if err != nil {
		return err
	}

	for _, image := range images {
		err = config.cli.CmdRmImage(image.ID)
		if err != nil {
			flog.Debugf(err.Error())
		}
	}

	return nil
}
