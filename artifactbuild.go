package pickett

import (
	"errors"
	"fmt"
	"github.com/igneous-systems/pickett/io"
	"time"
)

// ArtifactWorker implements the worker interface so it can be part of a node.
type artifactWorker struct {
	tag       string
	runIn     Node
	mergeWith Node
	artifacts map[string]string
}

// IsOutOfDate returns true if the tag that we are trying to produce is
// before the tag of the image we depend on.
func (a *artifactWorker) ood(conf *Config, helper io.Helper, cli io.DockerCli,
	etcd io.EtcdClient, vbox io.VirtualBox) (time.Time, bool, error) {
	t, err := tagToTime(a.tag, cli)
	if err != nil {
		return time.Time{}, true, err
	}
	if t.IsZero() {
		fmt.Printf("[pickett] Building %s (tag not found)\n", a.tag)
		return time.Time{}, true, nil
	}
	if t.Before(a.runIn.Time()) {
		fmt.Printf("[pickett] Building %s (out of date with respect to %s)\n", a.tag, a.runIn.Name())
		return time.Time{}, true, nil
	}
	if t.Before(a.mergeWith.Time()) {
		fmt.Printf("[pickett] Building %s (out of date with respect to %s)\n", a.tag, a.runIn.Name())
		return time.Time{}, true, nil
	}
	fmt.Printf("[pickett] '%s' is up to date\n", a.tag)
	return t, false, nil
}

func (a *artifactWorker) build(conf *Config, helper io.Helper, cli io.DockerCli,
	etcd io.EtcdClient, vbox io.VirtualBox) (time.Time, error) {
	if conf.CodeVolume.MountedAt == "" {
		return time.Time{}, errors.New("not clever enough to copy artifacts that are not on a code volume!")
	}
	dir := helper.DirectoryRelative(conf.CodeVolume.Directory)
	curr := a.mergeWith.Name()

	for k, v := range a.artifacts {
		runCmd := []string{
			"-v",
			fmt.Sprintf("%s:%s", dir, conf.CodeVolume.MountedAt),
			fmt.Sprintf("%s", curr),
			"cp",
			fmt.Sprintf("%s", k),
			fmt.Sprintf("%s", v),
		}
		helper.Debug("copying artifact with cp: %s -> %s", k, v)
		err := cli.CmdRun(false, runCmd...)
		if err != nil {
			return time.Time{}, err
		}
		err = cli.CmdPs("-q", "-l")
		if err != nil {
			return time.Time{}, err
		}
		err = cli.CmdCommit(cli.LastLineOfStdout())
		if err != nil {
			return time.Time{}, err
		}
		curr = cli.LastLineOfStdout()
	}
	err := cli.CmdTag(curr, a.tag)
	if err != nil {
		return time.Time{}, err
	}
	insp, err := cli.DecodeInspect(a.tag)
	if err != nil {
		return time.Time{}, err
	}
	helper.Debug("done copying, time for %s is %v", a.tag, insp.CreatedTime())
	return insp.CreatedTime(), nil
}

func (a *artifactWorker) in() []Node {
	return []Node{
		a.runIn, a.mergeWith,
	}
}
