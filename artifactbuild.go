package pickett

import (
	"fmt"
	"time"

	"github.com/igneoussystems/pickett/io"
)

// ArtifactBuildNode has a dependecy between the object to build and the two image
// that are used to build it.  This implements the Node interface.
type ArtifactBuildNode struct {
	runIn     Node
	mergeWith Node
	out       []Node
	tag       string
	tagTime   time.Time
	artifacts []string
}

// IsOutOfDate returns true if the tag that we are trying to produce is
// before the tag of the image we depend on.
func (a *ArtifactBuildNode) IsOutOfDate(conf *Config, helper io.IOHelper, cli io.DockerCli) (bool, error) {
	t, err := tagToTime(a.tag, cli)
	if err != nil {
		return false, err
	}
	if t.IsZero() {
		fmt.Printf("[pickett] Building %s (tag not found)\n", a.tag)
		return true, nil
	}
	if t.Before(a.runIn.Time()) {
		fmt.Printf("[pickett] Building %s (out of date with respect to %s)\n", a.tag, a.runIn.Name())
		return true, nil
	}
	if t.Before(a.mergeWith.Time()) {
		fmt.Printf("[pickett] Building %s (out of date with respect to %s)\n", a.tag, a.runIn.Name())
		return true, nil
	}
	fmt.Printf("[pickett] %s is up to date\n", a.tag)
	return false, nil
}

func (a *ArtifactBuildNode) build(conf *Config, helper io.IOHelper, cli io.DockerCli) error {

	return nil
}

//Build odes the actual work of gettig the artifact and then
//placing it in the 2nd conatiner
func (a *ArtifactBuildNode) Build(conf *Config, helper io.IOHelper, cli io.DockerCli) error {
	helper.Debug("Building (%s) ...", a.Name())
	return nil
}

//IsSink is true if this node has no outbound edges.
func (a *ArtifactBuildNode) IsSink() bool {
	return len(a.out) == 0
}

//BringInboundUpToDate walks all the nodes that this node depends on
//up to date.
func (a *ArtifactBuildNode) BringInboundUpToDate(conf *Config, helper io.IOHelper, cli io.DockerCli) error {
	return nil
}

//AddOut adds an outgoing edge from this node.
func (a *ArtifactBuildNode) AddOut(n Node) {
	a.out = append(a.out, n)
}

//Name prints the name of this node for a human to consume
func (a *ArtifactBuildNode) Name() string {
	return a.tag
}

//Time returns the most recent build time.
func (a *ArtifactBuildNode) Time() time.Time {
	return a.tagTime
}
