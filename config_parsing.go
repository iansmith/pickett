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
func (c *Config) checkContainerNodes() error {
	for _, img := range c.Containers {
		w, err := c.newContainerBuilder(img, c.helper)
		if err != nil {
			return err
		}
		if err := c.checkExistingName(img.Tag, true); err != nil {
			return err
		}
		node := newNodeImpl(w)
		c.nameToNode[w.tag()] = node
	}
	//make a pass adding edges
	for _, img := range c.Containers {
		dest := c.nameToNode[img.Repository+":"+img.Tag]
		work := dest.implementation().(*containerBuilder)
		for _, source := range img.DependsOn {
			node_source, ok := c.nameToNode[source]
			if !ok {
				return fmt.Errorf("image %s depends on %s, but %s not found",
					img.Repository+":"+img.Tag, source, source)
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
func (c *Config) checkGoBuildNodes() (map[*goBuilder]string, error) {
	implementations := make(map[*goBuilder]string)
	for _, build := range c.GoBuilds {
		w, err := c.newGoBuilder(build)
		if err != nil {
			return nil, err
		}
		if err := c.checkExistingName(build.Tag, true); err != nil {
			return nil, err
		}
		node := newNodeImpl(w)
		c.nameToNode[w.tag()] = node
		implementations[w] = strings.Trim(build.RunIn, " \n")
	}
	return implementations, nil
}

// stage2BuildNodes is because we need to have the possibility of dependency
// edges that are on networks or other gobuild nodes.
func (c *Config) dependenciesGoBuildNodes(implementations map[*goBuilder]string) error {
	for w, runIn := range implementations {
		r, found := c.nameToNode[runIn]
		if !found {
			return fmt.Errorf("Unable to find '%s' trying to build '%s': maybe you need to 'docker pull' it?",
				runIn, w.tag())
		}
		//add edges
		w.runIn = r
		node := c.nameToNode[w.tag()]
		r.addOut(node)
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
	_, err := cli.InspectImage(strings.Trim(tag, " \n"))
	return err == nil
}

// checkExtractionNodes verifies the simple portion of the extract nodes.  This does
// not introduce edges as that requires that all the nodes be known.
func (c *Config) checkExtractionNodes() (map[*extractionBuilder][]string, error) {
	implementations := make(map[*extractionBuilder][]string)
	for _, build := range c.Extractions {
		w, err := c.newExtractionBuilder(build)
		if err != nil {
			return nil, err
		}
		if err := c.checkExistingName(w.tag(), true); err != nil {
			return nil, err
		}
		//put it in the list
		node := newNodeImpl(w)
		c.nameToNode[w.tag()] = node
		implementations[w] = []string{}

		mergeTrimmed := strings.Trim(build.MergeWith, " \n")
		inTrimmed := strings.Trim(build.RunIn, " \n")
		if mergeTrimmed == "" || inTrimmed == "" {
			return nil, fmt.Errorf("MergeWith and RunIn are required for extractions!")
		}
		// the order of this append matters!
		implementations[w] = append(implementations[w], inTrimmed, mergeTrimmed)
	}
	return implementations, nil
}

//dependenciesExtractNodes is the 2nd part of the extraction node construction.
//In this phose we deal with the edges that may be needed to other nodes in the graph.
func (c *Config) dependenciesExtractNodes(implementations map[*extractionBuilder][]string) error {
	for extract, cand := range implementations {
		//order dependent on the list of size 2 in cand!
		in, merge := cand[0], cand[1]

		//incoming from runIn
		if !c.tagExists(in, c.cli) {
			return fmt.Errorf("Unable to find '%s' (RunIn) in extract build  '%s': maybe you need to 'docker pull' it?",
				in, extract.tag())
		}
		r, found := c.nameToNode[in]
		n := nodeOrName{name: in}
		if found {
			n.isNode = true
			n.node = r
		}
		extract.runIn = n

		//incoming from mergeWith
		if !c.tagExists(merge, c.cli) {
			return fmt.Errorf("Unable to find '%s' (MergeWith) in extract build '%s': maybe you need to 'docker pull' it?",
				merge, extract.tag())
		}
		m, found := c.nameToNode[merge]
		n = nodeOrName{name: merge}
		if found {
			n.isNode = true
			n.node = m
		}
		extract.mergeWith = n

		//put in outgoings, if needed
		node := c.nameToNode[extract.tag()]
		if extract.runIn.isNode {
			r.addOut(node)
		}
		if extract.mergeWith.isNode {
			m.addOut(node)
		}
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

//checkNetworkNodes verifies the easy part of all the network setups in this configuration file.
//Thes does the portion that does not have dependencies and returns the necessary
//bookkeeping for that to be done in a later pass.
func (c *Config) checkNetworkNodes() (map[*networkRunner]string, error) {
	commiters := make(map[string]*outcomeProxyBuilder)
	implementations := make(map[*networkRunner]string)

	//first pass is to establish all the names and do things that don't involve
	//complex deps of any kind
	for _, n := range c.Networks {
		if err := c.checkExistingName(n.Name, false); err != nil {
			return nil, err
		}
		w, err := c.newNetworkRunner(n)
		if err != nil {
			return nil, err
		}
		trimmedIn := strings.Trim(n.RunIn, " \n")
		implementations[w] = trimmedIn

		c.nameToNetwork[strings.Trim(n.Name, " \n")] = w

		//this is the way we build nodes that are really "part of" this
		//network runner but after it completes
		for input, result := range n.CommitOnExit {
			i := strings.Trim(input, " \n")
			resultTrimmed := strings.Trim(result, " \n")
			if !contains(n.Consumes, i) {
				return nil, fmt.Errorf("can't commit input %s in '%s' because it's not consumed",
					i, w.name())
			}
			parts := strings.Split(resultTrimmed, ":")
			if len(parts) != 2 {
				return nil, fmt.Errorf("can't understand commit result name '%s' expected something like foo:bar", result)
			}
			p := &outcomeProxyBuilder{
				net:        w,
				inputName:  i,
				repository: parts[0],
				tagname:    parts[1],
			}
			//leave a breadcrump
			commiters[i] = p
			//put in list of nodes
			c.nameToNode[resultTrimmed] = newNodeImpl(p)
		}
	}
	//second pass is to handle the possibility that network nodes reference
	//each other in the consumes section of the declaration
	for _, net := range c.Networks {
		simpleRunner := c.nameToNetwork[strings.Trim(net.Name, " \n")]
		n := simpleRunner.(*networkRunner)
		for _, in := range net.Consumes {
			trimmed := strings.Trim(in, " \n")
			other, ok := c.nameToNetwork[trimmed]
			if !ok {
				return nil, fmt.Errorf("can't find other network node named %s for %s", in, n.name())
			}
			n.consumes = append(n.consumes, other)
		}
	}

	return implementations, nil
}

//this works out to the third pass threough the network section.  this is to allow
//allow the possibility that the networks can reference each other and can reference
//the gobuild nodes.
func (c *Config) dependenciesNetworkNodes(implementations map[*networkRunner]string) error {
	//walk the know networks
	for n, runIn := range implementations {
		if !c.tagExists(runIn, c.cli) {
			return fmt.Errorf("unable to find image '%s' to run (network) %s in!", runIn, n.name())
		}
		n.runIn.name = runIn
		node, ok := c.nameToNode[runIn]
		if ok {
			n.runIn.node = node
			n.runIn.isNode = true
		}
	}
	return nil
}

//newNetworkRunner creates a new networkRunner node from the data supplied. It can fail if
//the config file is bogus; this ignores the issue of dependencies.
func (c *Config) newNetworkRunner(n *Network) (*networkRunner, error) {
	exp := make(map[pickett_io.Port][]pickett_io.PortBinding)

	//convert to the pickett_io format
	for k, v := range n.Expose {
		key := pickett_io.Port(k)
		curr, ok := exp[key]
		if !ok {
			curr = []pickett_io.PortBinding{}
		}
		var b pickett_io.PortBinding
		b.HostIp = "127.0.0.1"
		b.HostPort = fmt.Sprintf("%d", v)
		exp[key] = append(curr, b)
	}

	result := &networkRunner{
		n:      n.Name,
		expose: exp,
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
		tagname:    strings.Trim(src.Tag, "\n "),
		dir:        strings.Trim(src.Directory, "\n "),
		repository: strings.Trim(src.Repository, "\n "),
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
	if build.Repository == "" || build.Tag == "" {
		return nil, fmt.Errorf("repository and tag are required for a go build")
	}
	result := &goBuilder{
		tagname:    strings.Trim(build.Tag, "\n "),
		repository: strings.Trim(build.Repository, "\n "),
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
	worker := &extractionBuilder{
		artifacts:  build.Artifacts,
		tagname:    strings.Trim(build.Tag, "\n "),
		repository: strings.Trim(build.Repository, "\n "),
	}
	return worker, nil
}
