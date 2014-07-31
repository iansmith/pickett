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
	Tag       string
	Directory string
	DependsOn []string
}

type CodeVolume struct {
	Directory string
	MountedAt string
}

type GoBuild struct {
	Command  string
	RunIn    string
	Tag      string
	Packages []string
	TestFile string
	Probe    string
}

type GenericBuild struct {
	RunIn string
	Tag   string
	Run   []string
}

type Extraction struct {
	RunIn     string
	MergeWith string
	Tag       string
	Artifacts map[string]interface{}
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

type Config struct {
	DockerBuildOptions []string
	CodeVolume         CodeVolume
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
	helper.CheckFatal(err, "could not read all of configuration file: %v")
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
	conf.nameToNode = make(map[string]node)
	conf.nameToNetwork = make(map[string]runner)
	checks := []func(pickett_io.Helper, pickett_io.DockerCli) error{
		conf.checkContainerNodes,
		conf.checkGoBuildNodes,
		conf.checkExtractionNodes,
		conf.checkNetworks,
	}

	for _, fn := range checks {
		if err := fn(helper, cli); err != nil {
			return nil, err
		}
	}

	//save the objects
	conf.helper = helper
	conf.cli = cli
	conf.etcd = etcd
	conf.vbox = vbox

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
		fmt.Printf("[pickett] nothing to do for '%s'\n", node.name())
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
	rez, deps, err := net.run(true, c)
	fmt.Printf("execute finished : %+v, %+v\n", rez, deps)
	return err
}
