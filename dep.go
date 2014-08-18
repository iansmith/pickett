package pickett

import (
	"time"

	"github.com/igneous-systems/pickett/io"
)

//namer is something that knows its own name
type namer interface {
	name() string
}

//builder is the specific portion of a node that understands the semantics of the particular
//node type.  Node is the shared part.   The returned time value is _only_ used in the case
//of the ood() method when there is no error and the result is false.  The time, in the case
//of either the ood() with false and no error, or build() with no error, becomes the timestamp
//for this node.  This is to insure we don't bother even considering a node OOD if it has
//already been built or checked in the current process.
type builder interface {
	ood(*Config) (time.Time, bool, error)
	build(*Config) (time.Time, error)
	in() []node
	tag() string
}

//runners are things that know how to execute themselves.  They are generally not part of the
//same dependency graph that builders are part of because they are executed, not built. They
//can appear in the dependency graph if their *result* is consumed by some other build step.
//Right now, the only place this abstraction is really used is in policy.
type runner interface {
	namer
	//this returns a map of the results, as containers
	run(bool, *Config) (*policyInput, error)

	//some misc params for the run
	imageName() string
	exposed() map[io.Port][]io.PortBinding
	devices() map[string]string
	entryPoint() []string

	//note that this method is not really asking a question of the runner, it's asking a
	//question about the *image* that the runner executes in
	imageIsOutOfDate(*Config) (bool, error)

	//again,this is building the image the runner runs in, not the runner itself
	imageBuild(*Config) error
}

//node is the abstraction for an element in the dependency graph of builders.
//The key operations that are specific to the type of node are implemented
//by a delegate, but this is the "larger" API.
type node interface {
	namer
	isOutOfDate(*Config) (bool, error)
	build(*Config) error
	isSink() bool
	time() time.Time
	addOut(node) //don't need AddIn because the creator of Node handles that.
	implementation() builder
}

//nodeImpl implements the Node interface and has hooks for a builder.  This is the shared
//implementation between builders (really a kind of abstract base class).
type nodeImpl struct {
	b       builder
	out     []node
	tagTime time.Time
}

//newNodeImpl return a new Node that uses a specific builder implementation.
func newNodeImpl(b builder) node {
	return &nodeImpl{
		b: b,
	}
}

// isOutOfDate delegates to builder OOD function, but does bookkeeping about it at the top level.
func (n *nodeImpl) isOutOfDate(conf *Config) (bool, error) {
	//we have already done the work on this build?
	if !n.tagTime.IsZero() {
		conf.helper.Debug("Avoiding second check on %s (already found %v)", n.name(), n.tagTime)
		return false, nil
	}

	//if my inbound edges are ood, I am ood
	for _, in := range n.b.in() {
		ood, err := in.isOutOfDate(conf)
		if err != nil {
			return false, err
		}
		if ood {
			return true, nil
		}
	}

	//I'm not OOD because of recursive calls, so check my specific node type impl
	t, ood, err := n.b.ood(conf)
	if err != nil {
		return false, err
	}
	if !ood {
		n.tagTime = t
	}
	return ood, nil
}

//Build delegates to the builder action function if there is any work to do.
func (n *nodeImpl) build(conf *Config) error {
	if !n.tagTime.IsZero() {
		conf.helper.Debug("No work to do for '%s'.", n.name())
		return nil
	}

	if len(n.b.in()) != 0 {
		conf.helper.Debug("Building dependencies of '%s' (%d)", n.name(), len(n.b.in()))
	}
	for _, in := range n.b.in() {
		if err := in.build(conf); err != nil {
			return err
		}
	}
	//there is work to do locally
	conf.helper.Debug("Building '%s'", n.name())
	t, err := n.b.build(conf)
	if err != nil {
		return err
	}
	n.tagTime = t
	return nil
}

//addOut adds an outgoing edge from this node.
func (n *nodeImpl) addOut(other node) {
	n.out = append(n.out, other)
}

//time returns the time associated with this node (roughly it's last creation time).
func (n *nodeImpl) time() time.Time {
	return n.tagTime
}

//name returns the name of this node, typically it's tag.
func (n *nodeImpl) name() string {
	return n.b.tag()
}

//isSink is true if this node has no outbound edges.
func (n *nodeImpl) isSink() bool {
	return len(n.out) == 0
}

//Access to the underyling builder.
func (n *nodeImpl) implementation() builder {
	return n.b
}
