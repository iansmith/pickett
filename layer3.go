package pickett

import (
	"errors"
	"fmt"
	"time"

	"github.com/igneous-systems/pickett/io"
)

type layer3WorkerRunner struct {
	name       string
	runImage   string
	runInNode  bool
	runIn      Node
	entryPoint []string
	consumes   []Node //XXX this should be type runner somehow
	policy     policy
	expose     map[string]int
}

//in returns a single node that is our inbound edge, the container we run in.
func (l *layer3WorkerRunner) in() []Node {
	result := []Node{}
	if l.runInNode {
		result = append(result, l.runIn)
	}
	return result
}

func (l *layer3WorkerRunner) run(teeOutput bool, helper io.Helper, cli io.DockerCli, etcd io.EtcdClient,
	vbox io.VirtualBox) (*policyInput, error) {

	links := make(map[string]string)
	for _, node := range l.consumes {
		helper.Debug("launching %s because %s consumes it", node.Name(), l.name)
		r, ok := node.Worker().(*layer3WorkerRunner)
		if !ok {
			return nil, errors.New(
				fmt.Sprintf("%s: can't consume anything other than l3 services: %s", l.name, node.Name()))
		}
		input, err := r.run(false, helper, cli, etcd, vbox)
		if err != nil {
			return nil, err
		}
		links[input.containerName] = input.service.name
	}

	in, err := createPolicyInput(l, helper, cli, etcd)
	if err != nil {
		return nil, err
	}
	return in, l.policy.appyPolicy(teeOutput, in, links, helper, cli, etcd)
}

// ood is never true, there is no way for us to be out of date.
func (l *layer3WorkerRunner) ood(conf *Config, helper io.Helper, cli io.DockerCli,
	etcd io.EtcdClient, vbox io.VirtualBox) (time.Time, bool, error) {
	helper.Debug("layer 3 node '%s' is always up to date", l.name)
	return time.Time{}, false, nil
}

// There is no work to do in terms of building this object
func (b *layer3WorkerRunner) build(conf *Config, helper io.Helper, cli io.DockerCli,
	etcd io.EtcdClient, vbox io.VirtualBox) (time.Time, error) {
	return time.Time{}, nil
}
