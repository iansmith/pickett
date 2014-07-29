package pickett

import (
	"fmt"
	"time"
)

//nodeOrName represents a labelled entity. It can be either a tag that must be in the local
//docker cache or a node that is part of our dependency graph.
type nodeOrName struct {
	name   string
	isNode bool
	node   node
}

//network is a DAG of nodes that also are runnable.  Note that network node may be called
//to build() which has the effect of only preparing it's dependencies.
type networkRunner struct {
	n        string
	runIn    nodeOrName
	entry    []string
	consumes []runner
	policy   policy
	expose   map[string]int
}

func (n *networkRunner) name() string {
	return n.n
}

func (n *networkRunner) exposed() map[string]int {
	return n.expose
}

func (n *networkRunner) entryPoint() []string {
	return n.entry
}

//in returns a single node that is our inbound edge, the container we run in.
func (n *networkRunner) in() []node {
	result := []node{}
	if n.runIn.isNode {
		result = append(result, n.runIn.node)
	}
	return result
}

//imageName returns the image name needed to run this network.
func (n *networkRunner) imageName() string {
	return n.runIn.name
}

// run actually does the work to launch this network ,including launching all the networks
// that this one depends on (consumes).  Note that behavior of starting or stopping
// particular dependent services is controllled through the policy apparatus.
func (n *networkRunner) run(teeOutput bool, conf *Config) (*policyInput, map[string]*policyInput, error) {
	links := make(map[string]string)
	var results map[string]*policyInput
	if len(n.consumes) > 0 {
		results = make(map[string]*policyInput)
	}
	for _, r := range n.consumes {
		conf.helper.Debug("launching %s because %s consumes it", r.name(), n.name())
		input, deps, err := r.run(false, conf)
		if err != nil {
			return nil, nil, err
		}
		links[input.containerName] = input.r.name()
		results[r.name()] = input
		fmt.Printf("deps in run...discarded? %+v\n", deps)
	}

	in, err := createPolicyInput(n, conf)
	if err != nil {
		return nil, nil, err
	}
	return in, results, n.policy.appyPolicy(teeOutput, in, links, conf)
}

// imageIsOutOfDate delegates to the image if it is a node, otherwise false.
func (n *networkRunner) imageIsOutOfDate(conf *Config) (bool, error) {
	if !n.runIn.isNode {
		conf.helper.Debug("'%s' can't be out of date, image '%s' is not buildable", n.name(), n.runIn.name)
		return false, nil
	}
	return n.runIn.node.isOutOfDate(conf)
}

// we build the image if indeed that is possible
func (n *networkRunner) imageBuild(conf *Config) error {
	if !n.runIn.isNode {
		fmt.Printf("[pickett WARNING] '%s' can't be built, image '%s' is not buildable", n.name(), n.runIn.name)
		return nil
	}
	return n.runIn.node.build(conf)
}

type outcomeProxyBuilder struct {
	net         *networkRunner
	inputName   string
	inputRunner runner
	imageResult string
}

func (o *outcomeProxyBuilder) ood(conf *Config) (time.Time, bool, error) {
	ood, err := o.net.imageIsOutOfDate(conf)
	if ood || err != nil {
		return time.Time{}, ood, err
	}
	//evil, why is this abstraction breaking necessary?
	return o.net.runIn.node.time(), false, nil
}

func (o *outcomeProxyBuilder) build(conf *Config) (time.Time, error) {
	err := o.net.imageBuild(conf)
	if err != nil {
		return time.Time{}, err
	}
	in, deps, err := o.net.run(true, conf)
	if err != nil {
		return time.Time{}, err
	}
	fmt.Printf("should be doing the commit magic here...tear down and commit: %v, discard in? %v\n", deps, in)
	return time.Time{}, err
}

func (o *outcomeProxyBuilder) in() []node {
	result := []node{}
	if o.net.runIn.isNode {
		return append(result, o.net.runIn.node)
	}
	return result
}
