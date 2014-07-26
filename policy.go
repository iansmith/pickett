package pickett

import (
	"fmt"
	"path/filepath"
	"time"

	docker_utils "github.com/dotcloud/docker/utils"

	"github.com/igneous-systems/pickett/io"
)

//input for a policy to make a decision
type policyInput struct {
	hasStarted       bool
	containerName    string
	containerStarted time.Time
	isRunning        bool
	service          *layer3WorkerRunner
}

//formContainerKey is a helper for forming the keyname in etcd that corresponds
//to a particular service's container.
func formContainerKey(l *layer3WorkerRunner) string {
	return filepath.Join(io.PICKETT_KEYSPACE, CONTAINERS, l.name)
}

func formImageNameFromService(l *layer3WorkerRunner) string {
	if l.runInNode {
		return l.runIn.Name()
	}
	return l.runImage
}

//start runs the service in it's policy input and records the docker container
//name into etcd.
func (p *policyInput) start(teeOutput bool, image string, links map[string]string, cli io.DockerCli, etcd io.EtcdClient) error {
	args := []string{}
	if !teeOutput {
		args = append(args, "-d")
	} else {
		args = append(args, "-i", "-t")
	}
	for k, v := range links {
		args = append(args, "--link", fmt.Sprintf("%s:%s", k, v))
	}
	for k, v := range p.service.expose {
		args = append(args, "-p", fmt.Sprintf("%s:%d:%d", k, v, v))
	}
	args = append(append(args, image), p.service.entryPoint...)
	err := cli.CmdRun(teeOutput, args...)
	if err != nil {
		return err
	}
	if !teeOutput {
		id := cli.LastLineOfStdout()
		insp, err := cli.DecodeInspect(id)
		if err != nil {
			return err
		}
		if _, err = etcd.Put(formContainerKey(p.service), insp.ContainerName()); err != nil {
			return err
		}
		p.containerName = insp.ContainerName()
	}
	return nil
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
	CONTINUE
	RESTART
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
	case CONTINUE:
		return "CONTINUE"
	case RESTART:
		return "RESTART"
	}
	panic("unknown policy")
}

//applyPolicy takes a given policy and starts or stops containers as appropriate.
func (p policy) appyPolicy(teeOutput bool, in *policyInput, links map[string]string, helper io.Helper, cli io.DockerCli, etcd io.EtcdClient) error {
	if !in.hasStarted {
		helper.Debug("policy %s, initial start of %s", p, in.service.name)
		return in.start(teeOutput, formImageNameFromService(in.service), links, cli, etcd)
	}
	switch p {
	case ALWAYS:
		if in.isRunning {
			helper.Debug("policy %s, forcing stop of %s because currently running", p, in.service.name)
			if err := in.stop(cli, etcd); err != nil {
				return err
			}
		}
		helper.Debug("policy %s, starting %s", p, in.service.name)
		if err := in.start(teeOutput, formImageNameFromService(in.service), links, cli, etcd); err != nil {
			return err
		}
	case NEVER:
		if in.isRunning {
			helper.Debug("policy %s, not doing anything despite the service not running", p)
		}
	case FRESH:
		if !in.service.runInNode {
			helper.Debug("policy %s, container can't be out of date with respect to fixed image", p)
		}
		if !in.isRunning {
			helper.Debug("policy %s, starting %s because it's not running", p, in.service.name)
			if err := in.start(teeOutput, formImageNameFromService(in.service), links, cli, etcd); err != nil {
				return err
			}
		}
		if in.service.runInNode && (in.containerStarted.Before(in.service.runIn.Time())) {
			helper.Debug("policy %s, restarting due to freshness: container (%v) to image (%s)",
				p, in.containerStarted, in.service.runIn.Time())
			if err := in.stop(cli, etcd); err != nil {
				return err
			}
			if err := in.start(teeOutput, formImageNameFromService(in.service), links, cli, etcd); err != nil {
				return err
			}
		}
	case CONTINUE:
		if in.isRunning {
			helper.Debug("policy %s, container is running so not taking action")
		}
		if err := cli.CmdCommit(in.containerName); err != nil {
			return err
		}
		img := cli.LastLineOfStdout()
		if err := in.start(teeOutput, img, links, cli, etcd); err != nil {
			return err
		}
	case RESTART:
		if in.isRunning {
			helper.Debug("policy %s, container is running so not taking action")
		}
		if err := in.start(teeOutput, formImageNameFromService(in.service), links, cli, etcd); err != nil {
			return err
		}
	}
	return nil
}

//createPolicyInput does the work of interrogating etcd and if necessary docker to figure
//out the state of services.  It returns a policyInput suitable for applying policy to.
func createPolicyInput(l *layer3WorkerRunner, helper io.Helper, cli io.DockerCli, etcd io.EtcdClient) (*policyInput, error) {
	value, present, err := etcd.Get(formContainerKey(l))
	if err != nil {
		return nil, err
	}
	result := &policyInput{
		hasStarted:    present,
		containerName: value,
		service:       l,
	}
	// XXX this logic with the else clauses seems error prone
	if present {
		insp, err := cli.DecodeInspect(value)
		if err != nil {
			status, ok := err.(*docker_utils.StatusError)
			if ok && status.StatusCode == 1 {
				helper.Debug("ignoring docker container %s that is AWOL, probably was manually killed...", value)
				if _, err := etcd.Del(formContainerKey(l)); err != nil {
					return nil, err
				}
				result.isRunning = false
			} else {
				return nil, err
			}
		} else {
			result.isRunning = insp.Running()
			result.containerStarted = insp.CreatedTime()
		}
	}
	return result, nil
}
