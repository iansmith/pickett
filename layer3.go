package pickett

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/igneous-systems/pickett/io"
)

type layer3WorkerRunner struct {
	name       string
	runIn      Node
	entryPoint []string
	consumes   []Node //XXX this should be type runner somehow
}

//in returns a single node that is our inbound edge, the container we run in.
func (l *layer3WorkerRunner) in() []Node {
	return append(l.consumes, l.runIn)
}

func (l *layer3WorkerRunner) run(helper io.IOHelper, cli io.DockerCli, api io.EtcdClient) error {
	helper.Debug("starting run for %s", l.name)

	value, present, err := api.Get(filepath.Join(io.PICKETT_KEYSPACE, "containers", l.name))

	/*baseArgs := []string{"-d"}
	err := cli.CmdRun(false, l.runIn, l.entryPoint...)
	if err != nil {
		return err
	}
	dockerId := cli.LastLineOfStdout()
	*/
	fmt.Printf("l3 %v %v %v\n", value, present, err)
	return nil
}

//this allows us to start up a network of layer3 components
func (l *layer3WorkerRunner) XXXRun(teeoutput bool, helper io.IOHelper, cli io.DockerCli, api io.EtcdClient) (string, error) {
	/*	helper.Debug("starting invocation for %s", l.name)
		containerMap := make(map[string]string)
		for _, dependency := range l.consumes {
			r := dependency.Worker().(runner)
			id, err := r.run(false, helper, cli)
			if err != nil {
				return "", err
			}
			insp, err := cli.DecodeInspect(id)
			if err != nil {
				return "", err
			}
			containerMap[dependency.Name()] = insp.ContainerName()
		}
		baseArgs := []string{}
		if !teeoutput {
			baseArgs = append(baseArgs, "-d") //run in background
		}
		links := baseArgs
		for k, v := range containerMap {
			links = append(links, "--link", fmt.Sprintf("%s:%s", v, k))
		}
		cmd := append(append(links, l.runIn.Name()), l.entryPoint...)

		if !teeoutput {
			err := cli.CmdRun(false, cmd...)
			if err != nil {
				return "", err
			}
			return cli.LastLineOfStdout(), nil
		}
		err := cli.CmdRun(true, cmd...)
	*/
	return "", nil
}

// ood is never true, there is no way for us to be out of date.
func (l *layer3WorkerRunner) ood(conf *Config, helper io.IOHelper, cli io.DockerCli) (time.Time, bool, error) {
	helper.Debug("layer 3 node '%s' is always up to date", l.name)
	return time.Time{}, false, nil
}

// There is no work to do in terms of building this object
func (b *layer3WorkerRunner) build(conf *Config, helper io.IOHelper, cli io.DockerCli) (time.Time, error) {
	return time.Time{}, nil
}
