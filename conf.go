package pickett

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"strings"
)

type Source struct {
	Tag       string
	Directory string
	DependsOn []string
}

type CodeVolume struct {
	Directory string
	MountedAt string
}

type Build struct {
	//DependsOn        []string
	RunIn                    string
	Tag                      string
	InstallGoPackages        []string
	InstallAndTestGoPackages []string
	GenericRun               string
}

type Config struct {
	DockerBuildOptions []string
	CodeVolume         CodeVolume
	Sources            []*Source
	Builds             []*Build
	nameToNode         map[string]Node
}

// NewCofingFile creates a new instance of configuration, including
// all the parsing of the config file and validation checking on the
// items therein.
func NewConfig(reader io.Reader, helper IOHelper) (*Config, error) {
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
	conf.nameToNode = make(map[string]Node)
	conf.dockerSourceNodes(helper)
	conf.dockerBuildNodes()
	return conf, nil
}

// dockerSourceNodes walks all the nodes defined in the configuration file
// and returns them in a list.  The edges between the nodes are already
// in place when this function completes.
func (c *Config) dockerSourceNodes(helper IOHelper) error {

	for _, img := range c.Sources {
		n, err := c.newDockerSourceNode(img, helper)
		if err != nil {
			return err
		}
		c.nameToNode[img.Tag] = n
	}
	//make a pass adding edges
	for _, img := range c.Sources {
		dest := c.nameToNode[img.Tag].(*DockerSourceNode)
		for _, source := range img.DependsOn {
			node_source, ok := c.nameToNode[source]
			if !ok {
				return errors.New(fmt.Sprintf("image %s depends on %s, but %s not found",
					img.Tag, source, source))
			}
			node_source.AddOut(dest)
			dest.in = append(dest.in, node_source)
		}
	}
	return nil
}

// Sinks() return a list of the names of sinks you might want to build.
func (c *Config) Sinks() []string {
	result := []string{}
	for _, v := range c.nameToNode {
		if !v.IsSink() {
			continue
		}
		result = append(result, v.Name())
	}
	return result
}

// dockerBuildNodes returns all the build nodes in the pickett file.  Note that
// this should not be called until after the dockerSourceNodes() have been
// extracted as it needs data structures built at that stage.
func (c *Config) dockerBuildNodes() ([]*DockerBuildNode, error) {
	var result []*DockerBuildNode
	for _, build := range c.Builds {
		node, err := c.newDockerBuildNode(build)
		if err != nil {
			return nil, err
		}
		n, found := c.nameToNode[build.RunIn]
		if !found {
			return nil, err
		}
		node.runIn = n
		n.AddOut(node)
		result = append(result, node)
		c.nameToNode[build.Tag] = node
	}
	return result, nil
}

// newDockerSource returns a DockerSourceNode from the configuration information
// provided in the pickett file.  Note that this does some sanity checking of
// the provided directory so this can fail.  It uses the path to the
// Pickett.json file to construct paths such that the directory is relative
// to the place where the Pickett.json is located.  This ignores the issue
// of edges.
func (c *Config) newDockerSourceNode(src *Source, helper IOHelper) (*DockerSourceNode, error) {
	node := &DockerSourceNode{
		name: src.Tag,
		dir:  src.Directory,
	}
	_, err := helper.OpenDockerfileRelative(src.Directory)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("looked for %s/Dockerfile: %v",
			helper.DirectoryRelative(src.Directory), err))
	}
	return node, nil
}

// newDockerBuildNode returns a DockerBuildNode from the configuration information
// provided in the pickett file. This sanity checks the config file, so it can
// fail.
func (c *Config) newDockerBuildNode(build *Build) (*DockerBuildNode, error) {
	result := &DockerBuildNode{
		tag: build.Tag,
	}
	if len(build.InstallGoPackages) != 0 && len(build.InstallAndTestGoPackages) != 0 {
		return nil, errors.New(fmt.Sprintf("%s must define only one of InstallGoPackages and InstallAndTestGoPackages", build.Tag))
	}
	if len(build.InstallGoPackages) == 0 && len(build.InstallAndTestGoPackages) == 0 && build.GenericRun == "" {
		return nil, errors.New(fmt.Sprintf("%s must define one of InstallGoPackages,InstallAndTestGoPackages, and Generic Run", build.Tag))
	}
	if len(build.InstallGoPackages) != 0 {
		result.pkgs = build.InstallGoPackages
		result.test = false
	}
	if len(build.InstallAndTestGoPackages) != 0 {
		result.test = true
		result.pkgs = build.InstallAndTestGoPackages
	}
	if build.GenericRun != "" {
		result.generic = build.GenericRun
	}
	return result, nil
}

// Build does the work of running a particualur tag to creation.
func (c *Config) Build(name string, helper IOHelper) error {
	node, isPresent := c.nameToNode[name]
	if !isPresent {
		return errors.New(fmt.Sprintf("no such target: %s", name))
	}
	return node.Build(c, helper)
}
