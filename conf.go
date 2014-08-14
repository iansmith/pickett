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

type Network struct {
	Name         string
	RunIn        string
	EntryPoint   []string
	Consumes     []string
	Policy       string
	Expose       map[string]int
	CommitOnExit map[string]string
}

type BuildOpts struct {
	DontUseCache    bool
	RemoveContainer bool
}

type Config struct {
	DockerBuildOptions BuildOpts
	CodeVolumes        []*CodeVolume
	Containers         []*Container
	GoBuilds           []*GoBuild
	Extractions        []*Extraction
	GenericBuilds      []*GenericBuild
	Networks           []*Network

	//internal objects
	nameToNode    map[string]node
	nameToNetwork map[string]runner
	helper        pickett_io.Helper
	cli           pickett_io.DockerCli
	etcd          pickett_io.EtcdClient
	vbox          pickett_io.VirtualBox
}

// NewCofingFile creates a new instance of configuration, including
// all the parsing of the config file and validation checking on the
// items therein.
func NewConfig(reader io.Reader, helper pickett_io.Helper, cli pickett_io.DockerCli, etcd pickett_io.EtcdClient, vbox pickett_io.VirtualBox) (*Config, error) {
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
	conf.vbox = vbox

	//these are the two key OUTPUT datastructures when we are done with
	//all the parsing parts
	conf.nameToNode = make(map[string]node)
	conf.nameToNetwork = make(map[string]runner)

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
	netImpl, err := conf.checkNetworkNodes()
	if err != nil {
		return nil, err
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
	if err := conf.dependenciesNetworkNodes(netImpl); err != nil {
		return nil, err
	}
	if err := conf.dependenciesExtractNodes(extractImpl); err != nil {
		return nil, err
	}

	return conf, nil
}

// EntryPoints returns two lists, the list of buildable targets and the list of runnable
// targets.
func (c *Config) EntryPoints() ([]string, []string) {
	r1 := []string{}
	r2 := []string{}
	for k, _ := range c.nameToNode {
		r1 = append(r1, k)
	}
	for k, _ := range c.nameToNetwork {
		r2 = append(r2, k)
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

// Execute is called by the "main()" of the pickett program to run a "target".
func (c *Config) Execute(name string) error {
	net, isPresent := c.nameToNetwork[strings.Trim(name, " \n")]
	if !isPresent {
		return fmt.Errorf("no such target for build or run: %s", name)
	}
	_, err := net.run(true, c)
	return err
}

func (c *Config) codeVolumes() (map[string]string, error) {

	results := make(map[string]string)

	for _, v := range c.CodeVolumes {
		dir := c.helper.DirectoryRelative(v.Directory)
		path := dir
		var err error
		if c.vbox.NeedPathTranslation() {
			path, err = c.vbox.CodeVolumeToVboxPath(dir)
			if err != nil {
				return nil, err
			}
		}
		results[path] = v.MountedAt
	}
	return results, nil
}
