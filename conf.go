package pickett

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"strings"

	pickett_io "github.com/igneous-systems/pickett/io"
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
	Artifacts map[string]interface{}
}

type Layer3Service struct {
	Name       string
	RunIn      string
	EntryPoint []string
	Consumes   []string
}

type Config struct {
	DockerBuildOptions []string
	CodeVolume         CodeVolume
	Sources            []*Source
	GoBuilds           []*GoBuild
	ArtifactBuilds     []*ArtifactBuild
	GenericBuilds      []*GenericBuild
	Layer3Services     []*Layer3Service
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
	checks := []func(pickett_io.IOHelper) error{
		conf.checkSourceNodes,
		conf.checkGoBuildNodes,
		conf.checkArtifactBuildNodes,
		conf.checkLayer3Nodes,
	}

	for _, fn := range checks {
		if err := fn(helper); err != nil {
			return nil, err
		}
	}
	return conf, nil
}

//checkExistingNames is a helper to generate an error if the name is alerady
//in use in this configuration.
func (c *Config) checkExistingName(proposed string) error {
	if strings.Trim(proposed, " \n") == "" {
		return errors.New(fmt.Sprintf("can't have an empty name in configuration file"))
	}
	if _, ok := c.nameToNode[strings.Trim(proposed, " \n")]; ok {
		return errors.New(fmt.Sprintf("name %s already in use in this configuration"))
	}
	return nil
}

// checkSourceNodes walks all the "source" nodes defined in the configuration file.
// The edges between the nodes are already in place when this function completes.
func (c *Config) checkSourceNodes(helper pickett_io.IOHelper) error {
	for _, img := range c.Sources {
		w, err := c.newSourceWorker(img, helper)
		if err != nil {
			return err
		}
		if err := c.checkExistingName(img.Tag); err != nil {
			return err
		}
		node := newNodeImpl(img.Tag, w)
		c.nameToNode[img.Tag] = node
	}
	//make a pass adding edges
	for _, img := range c.Sources {
		dest := c.nameToNode[img.Tag]
		work := dest.Worker().(*sourceWorker)
		for _, source := range img.DependsOn {
			node_source, ok := c.nameToNode[source]
			if !ok {
				return errors.New(fmt.Sprintf("image %s depends on %s, but %s not found",
					img.Tag, source, source))
			}
			node_source.AddOut(dest)
			work.inEdges = append(work.inEdges, node_source)
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

// checkGoBuildNodes verifies all the "go build" nodes in this pickett file.  Note that
// this should not be called until after the checkSourceNodes() have been
// extracted as it needs data structures built at that stage.
func (c *Config) checkGoBuildNodes(pickett_io.IOHelper) error {
	for _, build := range c.GoBuilds {
		w, err := c.newGoWorker(build)
		if err != nil {
			return err
		}
		if err := c.checkExistingName(build.Tag); err != nil {
			return err
		}
		r, found := c.nameToNode[build.RunIn]
		if !found {
			return errors.New(fmt.Sprintf("Unable to find %s trying to build %s",
				build.RunIn, build.Tag))
		}

		//add edges
		w.runIn = r
		node := newNodeImpl(build.Tag, w)
		r.AddOut(node)

		c.nameToNode[build.Tag] = node
	}
	return nil
}

// checkArtifactBuildNodes verifies all the "artifact build" nodes in the pickett file.  Note that
// this should not be called until after the checkSourceNodes() and checkGoBuildNodes() have been
// extracted their parts, as it needs data structures built in these functions (notably dependencies).
func (c *Config) checkArtifactBuildNodes(pickett_io.IOHelper) error {
	for _, build := range c.ArtifactBuilds {
		w, err := c.newArtifactWorker(build)
		if err != nil {
			return err
		}
		if err := c.checkExistingName(build.Tag); err != nil {
			return err
		}
		r, found := c.nameToNode[build.RunIn]
		if !found {
			return errors.New(fmt.Sprintf("Unable to find '%s' trying to build RunIn '%s'",
				build.RunIn, build.Tag))
		}
		m, found := c.nameToNode[build.MergeWith]
		if !found {
			return errors.New(fmt.Sprintf("Unable to find '%s' trying to build MergeWith of  '%s'",
				build.MergeWith, build.Tag))
		}
		// handle dependency edges
		if _, ok := r.Worker().(*goWorker); !ok {
			return errors.New(fmt.Sprintf("Unable to create %s, right now artifacts must be derived from GoBuild nodes (%s is not GoBuild)",
				build.Tag, r.Name()))
		}
		w.runIn = r
		w.mergeWith = m
		node := newNodeImpl(build.Tag, w)
		r.AddOut(node)
		m.AddOut(node)

		c.nameToNode[build.Tag] = node
	}
	return nil
}

//checkLayer3Nodes verifies all the layer3 setups in this configuration file.
func (c *Config) checkLayer3Nodes(pickett_io.IOHelper) error {
	//first pass is to establish all the names and do things that don't involve
	//complex deps
	for _, l3 := range c.Layer3Services {
		if err := c.checkExistingName(l3.Name); err != nil {
			return err
		}
		w, err := c.newLayer3Worker(l3)
		if err != nil {
			return err
		}
		runIn, ok := c.nameToNode[strings.Trim(l3.RunIn, " \n")]
		if !ok {
			return errors.New(fmt.Sprintf("unable to find image '%s' to run %s in!", l3.RunIn, l3.Name))
		}
		w.runIn = runIn
		node := newNodeImpl(l3.Name, w)
		c.nameToNode[strings.Trim(l3.Name, " \n")] = node
	}
	//second pass is to introduce edges
	for _, l3 := range c.Layer3Services {
		sink := c.nameToNode[strings.Trim(l3.Name, " \n")]
		wr := sink.Worker().(*layer3WorkerRunner)

		for _, in := range l3.Consumes {
			other, ok := c.nameToNode[in]
			if !ok {
				return errors.New(fmt.Sprintf("can't find other layer3 service named %s for %s", in, l3.Name))
			}
			_, ok = other.Worker().(runner)
			if !ok {
				return errors.New(fmt.Sprintf("can't consume %s in %s, it's not a layer 3 service", in, l3.Name))
			}
			other.AddOut(sink)
			wr.consumes = append(wr.consumes, other)
		}
	}
	return nil
}

//newLayer3Worker creates a new layer3 node from the data supplied. It can fail if
//the config file is bogus; this ignores the issue of dependencies.
func (c *Config) newLayer3Worker(l3 *Layer3Service) (*layer3WorkerRunner, error) {
	result := &layer3WorkerRunner{
		name: l3.Name,
	}
	if len(l3.EntryPoint) == 0 {
		return nil, errors.New(fmt.Sprintf("cannot have an empty entry point (in %s)", l3.Name))
	}
	result.entryPoint = l3.EntryPoint
	return result, nil
}

// newSourceWorker returns a sourceWorker from the configuration information
// provided in the pickett file.  Note that this does some sanity checking of
// the provided directory so this can fail.  It uses the path to the
// Pickett.json file to construct paths such that the directory is relative
// to the place where the Pickett.json is located.  This ignores the issue
// of edges.
func (c *Config) newSourceWorker(src *Source, helper pickett_io.IOHelper) (*sourceWorker, error) {
	node := &sourceWorker{
		tag: src.Tag,
		dir: src.Directory,
	}
	_, err := helper.OpenDockerfileRelative(src.Directory)
	if err != nil {
		return nil, errors.New(fmt.Sprintf("looked for %s/Dockerfile: %v",
			helper.DirectoryRelative(src.Directory), err))
	}
	return node, nil
}

// newGoWorker returns a goWorker from the configuration information
// provided in the pickett file. This sanity checks the config file, so it can
// fail.  It ignores dependency edges.
func (c *Config) newGoWorker(build *GoBuild) (*goWorker, error) {
	result := &goWorker{
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

// newArtifactWorker returns a worker from the configuration information
// provided in the pickett file. This sanity checks the config file, so it can
// fail. It ignores dependency edges.
func (c *Config) newArtifactWorker(build *ArtifactBuild) (*artifactWorker, error) {
	if len(build.Artifacts) == 0 {
		return nil, errors.New(fmt.Sprintf("%s must define at least one artifact", build.Tag))
	}
	art := make(map[string]string)
	for k, v := range build.Artifacts {
		s, ok := v.(string)
		if !ok {
			return nil, errors.New(fmt.Sprintf("%v must be a string (in artifacts of %s)!", v, build.Tag))
		}
		art[k] = s
	}
	worker := &artifactWorker{
		artifacts: art,
		tag:       build.Tag,
	}
	return worker, nil
}

// Initiate does the work of running from creation to a particular tag being "born".
// Called by the "main()" of the pickett program if you provide a "target".
func (c *Config) Initiate(name string, helper pickett_io.IOHelper, cli pickett_io.DockerCli) error {
	node, isPresent := c.nameToNode[strings.Trim(name, " \n")]
	if !isPresent {
		return errors.New(fmt.Sprintf("no such target for build or run: %s", name))
	}
	err := node.Build(c, helper, cli)
	if err != nil {
		return err
	}
	//might be a node that can be run
	r, ok := node.Worker().(runner)
	if ok {
		//XXXX MOVE ME
		etcd := pickett_io.NewEtcdClient()
		err = r.run(helper, cli, etcd)
		if err != nil {
			return err
		}
	}
	return nil
}
