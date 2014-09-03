package pickett

import (
	"github.com/igneous-systems/pickett/io"
)

//nodeOrName represents a labelled entity. It can be either a tag that must be in the local
//docker cache or a node that is part of our dependency graph.
type nodeOrName struct {
	name      string
	isNode    bool
	node      node
	instances int
}

//topo runner is single node in a topology
type topoRunner struct {
	n             string
	runIn         nodeOrName
	entry         []string
	consumes      []runner
	policy        policy
	expose        map[io.Port][]io.PortBinding
	containerName string
	devs          map[string]string
	priv          bool
	wait          bool
}

func (n *topoRunner) name() string {
	return n.n
}

func (n *topoRunner) exposed() map[io.Port][]io.PortBinding {
	return n.expose
}

func (n *topoRunner) devices() map[string]string {
	return n.devs
}

func (n *topoRunner) privileged() bool {
	return n.priv
}

func (n *topoRunner) entryPoint() []string {
	return n.entry
}

func (n *topoRunner) waitFor() bool {
	return n.wait
}

func (n *topoRunner) contName() string {
	return n.containerName
}

//in returns a single node that is our inbound edge, the container we run in.
func (n *topoRunner) in() []node {
	result := []node{}
	if n.runIn.isNode {
		result = append(result, n.runIn.node)
	}
	return result
}

//imageName returns the image name needed to run this network.
func (n *topoRunner) imageName() string {
	return n.runIn.name
}

// run actually does the work to launch this network ,including launching all the networks
// that this one depends on (consumes).  Note that behavior of starting or stopping
// particular dependent services is controllled through the policy apparatus.
func (n *topoRunner) run(teeOutput bool, conf *Config,
	topoName string, topoEntry string, instance int,
	rv *runVolumeSpec) (*policyInput, error) {

	links := make(map[string]string)
	for _, r := range n.consumes {
		flog.Debugf("launching %s because %s consumes it (only launching one instance)", r.name(), n.name())
		input, err := r.run(false, conf, topoName, r.name(), 0, rv)
		if err != nil {
			return nil, err
		}
		links[input.containerName] = input.r.name()
	}

	in, err := createPolicyInput(n, topoName, topoEntry, instance, conf)
	if err != nil {
		return nil, err
	}
	n.containerName = in.containerName //for use in destroy
	return in, n.policy.appyPolicy(teeOutput, in, topoName, topoEntry, instance, links, rv, conf)
}

// imageIsOutOfDate delegates to the image if it is a node, otherwise false.
func (n *topoRunner) imageIsOutOfDate(conf *Config) (bool, error) {
	if !n.runIn.isNode {
		flog.Debugf("'%s' can't be out of date, image '%s' is not buildable", n.name(), n.runIn.name)
		return false, nil
	}
	return n.runIn.node.isOutOfDate(conf)
}

// we build the image if indeed that is possible
func (n *topoRunner) imageBuild(conf *Config) error {
	if !n.runIn.isNode {
		flog.Warningf("'%s' can't be built, image '%s' is not buildable", n.name(), n.runIn.name)
		return nil
	}
	return n.runIn.node.build(conf)
}
