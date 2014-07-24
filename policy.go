package pickett

import (
	"fmt"
	"github.com/igneous-systems/pickett/io"
	"path/filepath"
)

//input for a policy to make a decision
type policyInput struct {
	debug            bool
	hasStarted       bool
	containerName    string
	containerStarted time.Time
	isRunning        bool
	service          *layer3WorkerRunner
}

//debugf is useful for explanining to humans why a policy is doing something.
func (p *policyInput) debugf(s string, argv ...interface{}) {
	if p.debug {
		fmt.Printf("[policy] "+s+"\n", argv...)
	}
}

//formContainerKey is a helper for forming the keyname in etcd that corresponds
//to a particular service's container.
func formContainerKey(l *layer3WorkerRunner) string {
	return filepath.Join(io.PICKETT_KEYSPACE, CONTAINERS, l.name)
}

//start runs the service in it's policy input and records the docker container
//name into etcd.
func (p *policyInput) start(links map[string]string, cli io.DockerCli, etcd io.EtcdClient) error {
	var image string
	if p.service.runInNode {
		image = p.service.runIn.Name()
	} else {
		image = p.service.runImage
	}
	args := []string{
		"-d",
	}
	for k, v := range links {
		args = append(args, "--link", fmt.Sprintf("%s:%s", k, v))
	}
	args = append(append(args, image), p.service.entryPoint...)
	err := cli.CmdRun(false, args...)
	if err != nil {
		return err
	}
	id := cli.LastLineOfStdout()
	insp, err := cli.DecodeInspect(id)
	if err != nil {
		return err
	}
	_, err = etcd.Put(formContainerKey(p.service), insp.ContainerName())
	p.containerName = insp.ContainerName()
	return err
}

//stop stops the service in its policy input removes the container from etcd.
func (p *policyInput) stop(cli io.DockerCli, etcd io.EtcdClient) error {
	if err := cli.CmdStop(p.containerName); err != nil {
		return err
	}
	if _, err := etcd.Del(formContainerKey(p.service)); err != nil {
		return err
	}
	return nil
}

type policy int

const (
	ALWAYS policy = iota
	NEVER
	FRESH
)

const (
	CONTAINERS = "containers"
)

func (p policy) String() string {
	switch p {
	case ALWAYS:
		return "ALWAYS"
	case NEVER:
		return "NEVER"
	case FRESH:
		return "FRESH"
	}
	panic("unknown policy")
}

//applyPolicy takes a given policy and starts or stops containers as appropriate.
func (p policy) appyPolicy(in *policyInput, links map[string]string, cli io.DockerCli, etcd io.EtcdClient) error {
	if !in.hasStarted {
		in.debugf("policy %s, initial start of %s", p, in.service.name)
		return in.start(links, cli, etcd)
	}
	switch p {
	case ALWAYS:
		if in.isRunning {
			in.debugf("policy %s, forcing stop of %s because currently stopped", p, in.service.name)
			if err := in.stop(cli, etcd); err != nil {
				return err
			}
		}
		in.debugf("policy %s, starting %s", p, in.service.name)
		if err := in.start(links, cli, etcd); err != nil {
			return err
		}
	case NEVER:
		if in.isRunning {
			in.debugf("policy %s, not doing anything despite the service not running", p)
		}
	case FRESH:
		if !in.service.runInNode {
			in.debugf("policy %s can't be out of date with respect to fixed image", p)
		}
		if in.service.runInNode && (in.containerStarted.Before(in.service.runIn.Time())) {
			in.debugf("policy %s is restarting due to freshness: container (%v) to image (%s)",
				p, in.containerStarted, in.service.runIn.Time())
			if err := in.stop(links, cli, etcd); err != nil {
				return err
			}
			if err := in.start(links, cli, etcd); err != nil {
				return err
			}
		}
	}
	return nil
}

//createPolicyInput does the work of interrogating etcd and if necessary docker to figure
//out the state of services.  It returns a policyInput suitable for applying policy to.
func createPolicyInput(l *layer3WorkerRunner, cli io.DockerCli, etcd io.EtcdClient) (*policyInput, error) {
	value, present, err := etcd.Get(filepath.Join(io.PICKETT_KEYSPACE, CONTAINERS, l.name))
	if err != nil {
		return nil, err
	}
	result := &policyInput{
		hasStarted:    present,
		containerName: value,
		service:       l,
		debug:         true,
	}
	if present {
		insp, err := cli.DecodeInspect(value)
		if err != nil {
			return nil, err
		}
		result.isRunning = insp.Running()
		result.containerStarted = insp.CreatedTime()
	}
	fmt.Printf("create XXXX %+v\n", result)
	return result, nil
}
