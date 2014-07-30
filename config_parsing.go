package pickett

import (
	"fmt"
	"strings"

	pickett_io "github.com/igneous-systems/pickett/io"
)

//checkExistingNames is a helper to generate an error if the name is already
//in use in this configuration.  Pass true if you are interested in build nodes,
//false for networks.
func (c *Config) checkExistingName(proposed string, wantNode bool) error {
	p := strings.Trim(proposed, " \n")
	if p == "" {
		return fmt.Errorf("can't have an empty name in configuration file")
	}
	if wantNode {
		if c.nameToNode[p] != nil {
			return fmt.Errorf("name %s already in use in this configuration (build node)", p)
		}
	}
	if c.nameToNetwork[p] != nil {
		return fmt.Errorf("network name %s already in use in this configuration", p)
	}
	return nil
}

// checkContainerNodes walks all the "container" nodes defined in the configuration file.
// The edges between the nodes are in place when this function completes.
func (c *Config) checkContainerNodes(helper pickett_io.Helper, cli pickett_io.DockerCli) error {
	for _, img := range c.Containers {
		w, err := c.newContainerBuilder(img, helper)
		if err != nil {
			return err
		}
		if err := c.checkExistingName(img.Tag, true); err != nil {
			return err
		}
		node := newNodeImpl(img.Tag, w)
		c.nameToNode[img.Tag] = node
	}
	//make a pass adding edges
	for _, img := range c.Containers {
		dest := c.nameToNode[img.Tag]
		work := dest.implementation().(*containerBuilder)
		for _, source := range img.DependsOn {
			node_source, ok := c.nameToNode[source]
			if !ok {
				return fmt.Errorf("image %s depends on %s, but %s not found",
					img.Tag, source, source)
			}
			node_source.addOut(dest)
			work.inEdges = append(work.inEdges, node_source)
		}
	}
	return nil
}

// checkGoBuildNodes verifies all the "go build" nodes in this pickett file.  Note that
// this should not be called until after the checkSourceNodes() have been
// extracted as it needs data structures built at that stage.
func (c *Config) checkGoBuildNodes(pickett_io.Helper, pickett_io.DockerCli) error {
	for _, build := range c.GoBuilds {
		w, err := c.newGoBuilder(build)
		if err != nil {
			return err
		}
		if err := c.checkExistingName(build.Tag, true); err != nil {
			return err
		}
		r, found := c.nameToNode[build.RunIn]
		if !found {
			return fmt.Errorf("Unable to find %s trying to build %s: maybe you need to 'docker pull' it?",
				build.RunIn, build.Tag)
		}

		//add edges
		w.runIn = r
		node := newNodeImpl(build.Tag, w)
		r.addOut(node)
		c.nameToNode[build.Tag] = node
	}
	return nil
}

//check to see if a given image exists, it could be something we are going to construct
//it might just be in the docker cache or the docker repo
func (c *Config) tagExists(tag string, cli pickett_io.DockerCli) bool {
	_, ok := c.nameToNode[strings.Trim(tag, " \n")]
	if ok {
		return true
	}
	_, err := cli.DecodeInspect(strings.Trim(tag, " \n"))
	return err == nil
}

// checkExtractionNodes verifies all the "extract" nodes in the pickett file.  Note that
// this should not be called until after the checkContainerNodes() and checkGoBuildNodes() have been
// extracted their parts, as it needs data structures built in these functions
// (notably dependencies).
func (c *Config) checkExtractionNodes(helper pickett_io.Helper, cli pickett_io.DockerCli) error {
	for _, build := range c.Extractions {
		w, err := c.newExtractionBuilder(build)
		if err != nil {
			return err
		}
		if err := c.checkExistingName(build.Tag, true); err != nil {
			return err
		}
		if !c.tagExists(build.RunIn, cli) {
			return fmt.Errorf("Unable to find '%s' (RunIn) trying to setup for artifact build  '%s': maybe you need to 'docker pull' it?",
				build.RunIn, build.Tag)
		}
		r, found := c.nameToNode[build.RunIn]
		var in nodeOrName
		in.name = build.RunIn
		if found {
			in.isNode = true
			in.node = r
		}
		w.runIn = in

		if !c.tagExists(build.MergeWith, cli) {
			return fmt.Errorf("Unable to find '%s' (MergeWith) trying to setup for artifact '%s': maybe you need to 'docker pull' it?",
				build.MergeWith, build.Tag)
		}
		m, found := c.nameToNode[build.MergeWith]
		var merge nodeOrName
		merge.name = build.MergeWith
		if found {
			merge.isNode = true
			merge.node = m
		}
		//
		// handle dependency edges
		//
		_, ok := r.implementation().(*goBuilder)
		if w.runIn.isNode == false || !ok {
			return fmt.Errorf("Unable to create %s, right now artifacts must be derived from GoBuild nodes (%s is not GoBuild)",
				build.Tag, r.name())
		}
		node := newNodeImpl(build.Tag, w)
		if w.runIn.isNode {
			r.addOut(node)
		}
		if w.mergeWith.isNode {
			m.addOut(node)
		}
		c.nameToNode[strings.Trim(build.Tag, " \n")] = node
	}
	return nil
}

func contains(list []string, candidate string) bool {
	for _, s := range list {
		if candidate == s {
			return true
		}
	}
	return false
}

//checkNetworks verifies all the network setups in this configuration file.
func (c *Config) checkNetworks(helper pickett_io.Helper, cli pickett_io.DockerCli) error {
	commiters := make(map[string]*outcomeProxyBuilder)
	//first pass is to establish all the names and do things that don't involve
	//complex deps
	for _, n := range c.Networks {
		if err := c.checkExistingName(n.Name, false); err != nil {
			return err
		}
		w, err := c.newNetworkRunner(n)
		if err != nil {
			return err
		}
		trimmedIn := strings.Trim(n.RunIn, " \n")
		other, ok := c.nameToNode[trimmedIn]
		var in nodeOrName
		in.name = trimmedIn
		if ok {
			in.node = other
			in.isNode = true
		}
		w.runIn = in
		c.nameToNetwork[strings.Trim(n.Name, " \n")] = w
		for input, result := range n.CommitOnExit {
			i := strings.Trim(input, " \n")
			if !contains(n.Consumes, i) {
				return fmt.Errorf("can't commit input %s in '%s' because it's not consumed",
					i, w.name())
			}
			p := &outcomeProxyBuilder{
				net:         w,
				inputName:   i,
				imageResult: result,
			}
			//leave a breadcrump
			commiters[i] = p
			//put in list of nodes
			c.nameToNode[result] = newNodeImpl(result, p)
		}
	}

	//second pass is to introduce edges
	for _, net := range c.Networks {
		r := c.nameToNetwork[strings.Trim(net.Name, " \n")]
		n := r.(*networkRunner)

		//can't do this check until second pass
		if !c.tagExists(net.RunIn, cli) {
			return fmt.Errorf("unable to find image '%s' to run (network) %s in!", net.RunIn, net.Name)
		}

		for _, in := range net.Consumes {
			other, ok := c.nameToNetwork[in]
			if !ok {
				return fmt.Errorf("can't find other network node named %s for %s", in, n.name())
			}
			n.consumes = append(n.consumes, other)
			p, ok := commiters[n.name()]
			if ok {
				p.inputRunner = other
			}
		}
	}

	return nil
}

//newNetworkRunner creates a new networkRunner node from the data supplied. It can fail if
//the config file is bogus; this ignores the issue of dependencies.
func (c *Config) newNetworkRunner(n *Network) (*networkRunner, error) {
	result := &networkRunner{
		n:      n.Name,
		expose: n.Expose,
	}
	pol := defaultPolicy()
	switch strings.ToUpper(n.Policy) {
	case "BY_HAND":
		pol.startIfNonExistant = false
		pol.stop = NEVER
		pol.rebuildIfOOD = false
	case "KEEP_UP":
		pol.stop = NEVER
	case "CONTINUE":
		pol.stop = NEVER
		pol.start = CONTINUE
	case "FRESH", "": //we allow an empty string to mean FRESH
		//nothing to do, its all defaults
	case "ALWAYS":
		pol.stop = ALWAYS
	default:
		return nil, fmt.Errorf("unknown policy %s chosen for %s", n.Policy, n.Name)
	}
	result.policy = pol

	//copy entry point if provided
	result.entry = n.EntryPoint
	return result, nil
}

// newContainerBuilder returns a containerBuilder from the configuration information
// provided in the pickett file.  Note that this does some sanity checking of
// the provided directory so this can fail.  It uses the path to the
// Pickett.json file to construct paths such that the directory is relative
// to the place where the Pickett.json is located.  This ignores the issue
// of edges.
func (c *Config) newContainerBuilder(src *Container, helper pickett_io.Helper) (*containerBuilder, error) {
	node := &containerBuilder{
		tag: src.Tag,
		dir: src.Directory,
	}
	_, err := helper.OpenDockerfileRelative(src.Directory)
	if err != nil {
		return nil, fmt.Errorf("looked for %s/Dockerfile: %v",
			helper.DirectoryRelative(src.Directory), err)
	}
	return node, nil
}

// newGoBuilder returns a goBuilder from the configuration information
// provided in the pickett file. This sanity checks the config file, so it can
// fail.  It ignores dependency edges.
func (c *Config) newGoBuilder(build *GoBuild) (*goBuilder, error) {
	result := &goBuilder{
		tag: build.Tag,
	}
	if len(build.Packages) == 0 {
		return nil, fmt.Errorf("you must define at least one source package for a go build")
	}
	result.pkgs = build.Packages
	if build.Command != "" {
		result.command = build.Command
	} else {
		result.command = "go install"
	}
	if build.TestFile != "" {
		result.testFile = build.TestFile
	}
	if build.Probe != "" {
		result.probe = build.Probe
	} else {
		result.probe = "go install -n"
	}
	return result, nil
}

// newExtractionBuilder returns a worker from the configuration information
// provided in the pickett file. This sanity checks the config file, so it can
// fail. It ignores dependency edges.
func (c *Config) newExtractionBuilder(build *Extraction) (*extractionBuilder, error) {
	if len(build.Artifacts) == 0 {
		return nil, fmt.Errorf("%s must define at least one artifact", build.Tag)
	}
	art := make(map[string]string)
	for k, v := range build.Artifacts {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("%v must be a string (in artifacts of %s)!", v, build.Tag)
		}
		art[k] = s
	}
	worker := &extractionBuilder{
		artifacts: art,
		tag:       build.Tag,
	}
	return worker, nil
}
