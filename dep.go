package pickett

import (
	"time"

	"github.com/igneous-systems/pickett/io"
)

//worker is the specific portion of a node that understands the semantics of the particular
//node type.  Node is the shared part.   The returned time value is _only_ used in the case
//of the ood() method when there is no error and the result is false.  The time, in the case
//of either the ood() with false and no error, or build() with no error, becomes the timestamp
//for this node.  This is to insure we don't bother even considering a node OOD if it has
//already been built or checked in the current process.
type worker interface {
	ood(*Config, io.Helper, io.DockerCli, io.EtcdClient, io.VirtualBox) (time.Time, bool, error)
	build(*Config, io.Helper, io.DockerCli, io.EtcdClient, io.VirtualBox) (time.Time, error)
	in() []Node
}

//runners are things that know how to execute themselves.  The problem with the types
//here is some things are both runner and worker.
type runner interface {
	run(io.Helper, io.DockerCli, io.EtcdClient, io.VirtualBox) error
}

//Node is the abstraction for an element in the dependency graph.  The key operations that are
//specific to the type of node are implemented by a specific Worker.
type Node interface {
	IsOutOfDate(*Config, io.Helper, io.DockerCli, io.EtcdClient, io.VirtualBox) (bool, error)
	Build(*Config, io.Helper, io.DockerCli, io.EtcdClient, io.VirtualBox) error
	IsSink() bool
	BringInboundUpToDate(*Config, io.Helper, io.DockerCli, io.EtcdClient, io.VirtualBox) error
	Name() string
	Time() time.Time
	AddOut(Node) //don't need AddIn because the creator of Node handles that.
	Worker() worker
}

//nodeImpl implements the Node interface and has hooks for a Worker.
type nodeImpl struct {
	work    worker
	out     []Node
	tag     string
	tagTime time.Time
}

func newNodeImpl(name string, w worker) *nodeImpl {
	return &nodeImpl{
		work: w,
		tag:  name,
	}
}

// IsOutOfDate delegates to worker OOD function, but does bookkeeping about it at the top level.
func (n *nodeImpl) IsOutOfDate(conf *Config, helper io.Helper, cli io.DockerCli, etcd io.EtcdClient, vbox io.VirtualBox) (bool, error) {
	//we have already done the work on this build?
	if !n.tagTime.IsZero() {
		helper.Debug("avoiding second check on %s (already found %v)", n.Name(), n.tagTime)
		return false, nil
	}
	//no, need to do the work
	t, ood, err := n.work.ood(conf, helper, cli, etcd, vbox)
	if err != nil {
		return false, err
	}
	if !ood {
		n.tagTime = t
	}
	return ood, nil
}

//Build delegates to the worker build function if there is any work to do.
func (n *nodeImpl) Build(conf *Config, helper io.Helper, cli io.DockerCli, etcd io.EtcdClient, vbox io.VirtualBox) error {
	helper.Debug("Building '%s' ...", n.Name())
	err := n.BringInboundUpToDate(conf, helper, cli, etcd, vbox)
	if err != nil {
		return err
	}
	ood, err := n.IsOutOfDate(conf, helper, cli, etcd, vbox)
	if err != nil {
		return err
	}
	if !ood {
		//the particular worker will output a message, no need to duplicate
		return nil
	}

	//there is work to do
	t, err := n.work.build(conf, helper, cli, etcd, vbox)
	if err != nil {
		return err
	}
	n.tagTime = t
	return nil
}

//BringInboundUpToDate walks all the nodes that this node depends on
//up to date.
func (n *nodeImpl) BringInboundUpToDate(conf *Config, helper io.Helper, cli io.DockerCli, etcd io.EtcdClient, vbox io.VirtualBox) error {
	inbound := n.work.in()
	for _, in := range inbound {
		if err := in.Build(conf, helper, cli, etcd, vbox); err != nil {
			return err
		}
	}
	return nil
}

//AddOut adds an outgoing edge from this node.
func (n *nodeImpl) AddOut(other Node) {
	n.out = append(n.out, other)
}

//Time returns the time associated with this node (roughly it's last creation time).
func (n *nodeImpl) Time() time.Time {
	return n.tagTime
}

//Name returns the name of this node, typically it's tag.
func (n *nodeImpl) Name() string {
	return n.tag
}

//IsSink is true if this node has no outbound edges.
func (n *nodeImpl) IsSink() bool {
	return len(n.out) == 0
}

//Access to the underyling worker.
func (n *nodeImpl) Worker() worker {
	return n.work
}
