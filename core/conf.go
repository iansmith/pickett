package pickett

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"strings"

	pickett_io "github.com/igneous-systems/pickett/io"
)

type Container struct {
	Repository string
	Tag        string
	Directory  string
	DependsOn  []string
}

type CodeVolume struct {
	Directory string
	MountedAt string
}

type GoBuild struct {
	Command    string
	RunIn      string
	Repository string
	Tag        string
	Packages   []string
	TestFile   string
	Probe      string
}

type GenericBuild struct {
	RunIn string
	Tag   string
	Run   []string
}

type Artifact struct {
	BuiltPath      string
	DestinationDir string
}

type Extraction struct {
	Repository string
	RunIn      string
	MergeWith  string
	Tag        string
	Artifacts  []*Artifact
}

type TopologyEntry struct {
	Name       string
	RunIn      string
	EntryPoint []string
	Consumes   []string
	Policy     string
	Expose     map[string]int
	Instances  int
	Devices    map[string]string
	Privileged bool
	WaitFor    bool
}

type BuildOpts struct {
	DontUseCache    bool
	RemoveContainer bool
}

type topoInfo struct {
	runner    runner
	instances int
}

type Config struct {
	DockerBuildOptions BuildOpts
	CodeVolumes        []*CodeVolume
	Containers         []*Container
	GoBuilds           []*GoBuild
	Extractions        []*Extraction
	GenericBuilds      []*GenericBuild
	Topologies         map[string][]*TopologyEntry

	//internal objects
	nameToNode     map[string]node
	nameToTopology map[string]topoMap
	helper         pickett_io.Helper
	cli            pickett_io.DockerCli
	etcd           pickett_io.EtcdClient
}

type topoMap map[string]*topoInfo

// NewCofingFile creates a new instance of configuration, including
// all the parsing of the config file and validation checking on the
// items therein.
func NewConfig(reader io.Reader, helper pickett_io.Helper, cli pickett_io.DockerCli, etcd pickett_io.EtcdClient) (*Config, error) {
	all, err := ioutil.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("could not read all of configuration file: %v", err)
	}
	lines := strings.Split(string(all), "\n")
	var noComments bytes.Buffer
	for _, line := range lines {
		if index := strings.Index(line, "//"); index != -1 {
			if index == 0 {
				continue
			}
			line = line[:index]
		}
		noComments.WriteString(line)
	}

	//try to decode the json blob
	dec := json.NewDecoder(&noComments)
	conf := &Config{}
	err = dec.Decode(&conf)
	if err != nil {
		return nil, err
	}

	//save the objects and put them where the true configuration parsing
	//can see them
	conf.helper = helper
	conf.cli = cli
	conf.etcd = etcd

	//these are the two key OUTPUT datastructures when we are done with
	//all the parsing parts
	conf.nameToNode = make(map[string]node)
	conf.nameToTopology = make(map[string]topoMap)

	// PART 1: containers cannot reference anything other than containers,
	// PART 1: so we can just process them
	if err := conf.checkContainerNodes(); err != nil {
		return nil, err
	}

	//PART 2: Do the the simple portion of each of the three complex build
	//PART 2: types.  Note that this does no introduce edges because it
	//PART 2: may need all portsion of this to run before we would have the
	//PART 2: the node we need.  The order of these does not matter.
	goImpl, err := conf.checkGoBuildNodes()
	if err != nil {
		return nil, err
	}
	topos := make(map[string]map[*topoRunner]string)
	for top, entries := range conf.Topologies {
		t := strings.Trim(top, " \n")
		conf.nameToTopology[t] = make(map[string]*topoInfo)
		impl, err := conf.checkTopologyNodes(top, entries)
		if err != nil {
			return nil, err
		}
		topos[t] = impl
	}
	extractImpl, err := conf.checkExtractionNodes()
	if err != nil {
		return nil, err
	}

	//PART 3: We now have the full set of possible nodes, so we want to
	//PART 3: introduce edges between them.
	if err := conf.dependenciesGoBuildNodes(goImpl); err != nil {
		return nil, err
	}
	for t, topoImpl := range topos {
		if err := conf.dependenciesTopologyNodes(t, topoImpl); err != nil {
			return nil, err
		}
	}
	if err := conf.dependenciesExtractNodes(extractImpl); err != nil {
		return nil, err
	}

	return conf, nil
}

// EntryPoints returns two lists, the list of buildable targets and the list of runnable
// topologies.
func (c *Config) EntryPoints() ([]string, []string) {
	r1 := []string{}
	r2 := []string{}
	for k, _ := range c.nameToNode {
		r1 = append(r1, k)
	}
	for k, v := range c.nameToTopology {
		for n, _ := range v {
			r2 = append(r2, k+"."+n)
		}
	}
	return r1, r2
}

// Build is called by the "main()" of the pickett program to build a "target".
func (c *Config) Build(name string) error {
	node, isPresent := c.nameToNode[strings.Trim(name, " \n")]
	if !isPresent {
		return fmt.Errorf("no such target for build: %s", name)
	}
	ood, err := node.isOutOfDate(c)
	if err != nil {
		return err
	}
	if !ood {
		flog.Infof("nothing to do for '%s'", node.name())
		return nil
	}
	return node.build(c)
}
func (c *Config) ParseTopoNamesToContainerName(topoName string, name string) (string, *topoInfo, error) {
	pair := strings.Split(strings.Trim(name, " \n"), ".")
	if len(pair) != 2 {
		return "", nil, fmt.Errorf("unable to understand '%s', expect something like 'foo.bar'", name)
	}
	tmap, isPresent := c.nameToTopology[pair[0]]
	if !isPresent {
		return "", nil, fmt.Errorf("no such topology '%s'", pair[0])
	}
	var info *topoInfo
	for key, value := range tmap {
		if pair[1] == key {
			info = value
			break
		}
	}
	if info == nil {
		return "", nil, fmt.Errorf("unable to understand '%s', expected something like foo.bar (%s is ok)", pair[1], pair[0])
	}
	return fmt.Sprintf("%s.%s.", topoName, pair[1]), info, nil
}

// Execute is called by the "main()" of the pickett program to run a "target".
func (c *Config) Execute(topologyName string, name string, vol *runVolumeSpec) (int, error) {
	_, info, err := c.ParseTopoNamesToContainerName(topologyName, name)
	if err != nil {
		return 1, err
	}
	pair := strings.Split(name, ".") //checked in the ParseTopoNames, so no err worry
	exitStatus := 0
	for i := 0; i < info.instances; i++ {
		// wait on the last instance only, in case many are specificed.
		wait := info.runner.waitFor() && i == info.instances-1
		p, err := info.runner.run(wait, c, topologyName, pair[1], i, vol)
		if err != nil {
			return 1, err
		}
		if wait {
			insp, err := c.cli.InspectContainer(p.containerName)
			if err != nil {
				return 1, err
			}
			exitStatus = insp.ExitStatus()
		}
	}
	return exitStatus, nil
}

// codeVolumes returns a map from container-external to container-internal paths.
func (c *Config) codeVolumes() (map[string]string, error) {
	results := make(map[string]string)
	for _, v := range c.CodeVolumes {
		dir := c.helper.DirectoryRelative(v.Directory)
		var err error
		if needsPathTranslation() {
			dir, err = translatePath(dir)
			if err != nil {
				return nil, err
			}
		}
		results[dir] = v.MountedAt
	}
	return results, nil
}
