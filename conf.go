package pickett

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"strings"

	pickett_io "github.com/igneoussystems/pickett/io"
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

type GoBuild struct {
	RunIn                    string
	Tag                      string
	InstallGoPackages        []string
	InstallAndTestGoPackages []string
}

type GenericBuild struct {
	RunIn string
	Tag   string
	Run   []string
}

type ArtifactBuild struct {
	RunIn     string
	MergeWith string
	Tag       string
	Artifacts []string
}

type Config struct {
	DockerBuildOptions []string
	CodeVolume         CodeVolume
	Sources            []*Source
	GoBuilds           []*GoBuild
	ArtifactBuilds     []*ArtifactBuild
	GenericBuilds      []*GenericBuild
	nameToNode         map[string]Node
}

// NewCofingFile creates a new instance of configuration, including
// all the parsing of the config file and validation checking on the
// items therein.
func NewConfig(reader io.Reader, helper pickett_io.IOHelper) (*Config, error) {
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
	if err := conf.dockerSourceNodes(helper); err != nil {
		return nil, err
	}
	if _, err := conf.goBuildNodes(); err != nil {
		return nil, err
	}
	if _, err := conf.artifactBuildNodes(); err != nil {
		return nil, err
	}
	return conf, nil
}

// dockerSourceNodes walks all the nodes defined in the configuration file
// and returns them in a list.  The edges between the nodes are already
// in place when this function completes.
func (c *Config) dockerSourceNodes(helper pickett_io.IOHelper) error {
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

// goBuildNodes returns all the go build nodes in the pickett file.  Note that
// this should not be called until after the dockerSourceNodes() have been
// extracted as it needs data structures built at that stage.
func (c *Config) goBuildNodes() ([]*GoBuildNode, error) {
	var result []*GoBuildNode
	for _, build := range c.GoBuilds {
		node, err := c.newGoBuildNode(build)
		if err != nil {
			return nil, err
		}
		n, found := c.nameToNode[build.RunIn]
		if !found {
			return nil, errors.New(fmt.Sprintf("Unable to find %s trying to build %s",
				build.RunIn, build.Tag))
		}
		node.runIn = n
		n.AddOut(node)
		result = append(result, node)
		c.nameToNode[build.Tag] = node
	}
	return result, nil
}

// artifactBuildNodes returns all theartifact build nodes in the pickett file.  Note that
// this should not be called until after the dockerSourceNodes() have been
// extracted as it needs data structures built at that stage.
func (c *Config) artifactBuildNodes() ([]*ArtifactBuildNode, error) {
	var result []*ArtifactBuildNode
	for _, build := range c.ArtifactBuilds {
		node, err := c.newArtifactBuildNode(build)
		if err != nil {
			return nil, err
		}
		n, found := c.nameToNode[build.RunIn]
		if !found {
			return nil, errors.New(fmt.Sprintf("Unable to find %s trying to build %s",
				build.RunIn, build.Tag))
		}
		m, found := c.nameToNode[build.MergeWith]
		if !found {
			return nil, errors.New(fmt.Sprintf("Unable to find %s trying to build %s",
				build.MergeWith, build.Tag))
		}
		node.runIn = n
		n.AddOut(node)
		node.mergeWith = m
		m.AddOut(node)
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
func (c *Config) newDockerSourceNode(src *Source, helper pickett_io.IOHelper) (*DockerSourceNode, error) {
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

// newGoBuildNode returns a GoBuildNode from the configuration information
// provided in the pickett file. This sanity checks the config file, so it can
// fail.
func (c *Config) newGoBuildNode(build *GoBuild) (*GoBuildNode, error) {
	result := &GoBuildNode{
		tag: build.Tag,
	}
	if len(build.InstallGoPackages) != 0 && len(build.InstallAndTestGoPackages) != 0 {
		return nil, errors.New(fmt.Sprintf("%s must define only one of InstallGoPackages and InstallAndTestGoPackages", build.Tag))
	}
	if len(build.InstallGoPackages) != 0 {
		result.pkgs = build.InstallGoPackages
		result.test = false
	}
	if len(build.InstallAndTestGoPackages) != 0 {
		result.test = true
		result.pkgs = build.InstallAndTestGoPackages
	}
	return result, nil
}

// newArtifactBuildNode returns an ArtifactBuildNode from the configuration information
// provided in the pickett file. This sanity checks the config file, so it can
// fail.
func (c *Config) newArtifactBuildNode(build *ArtifactBuild) (*ArtifactBuildNode, error) {
	result := &ArtifactBuildNode{
		tag: build.Tag,
	}
	if len(build.Artifacts) == 0 {
		return nil, errors.New(fmt.Sprintf("%s must define at leasnt one artifact", build.Tag))
	}
	return result, nil
}

// Initiate does the work of running from creation to a particular tag being "born".
// Called by the "main()" of the pickett program if you provide a "target".
func (c *Config) Initiate(name string, helper pickett_io.IOHelper, cli pickett_io.DockerCli) error {
	node, isPresent := c.nameToNode[name]
	if !isPresent {
		return errors.New(fmt.Sprintf("no such target: %s", name))
	}
	return node.Build(c, helper, cli)
}
