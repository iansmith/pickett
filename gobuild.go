package pickett

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/igneous-systems/pickett/io"
)

// goworker has a dependecy between the object to build and the image
// that is used to build it.  This implements the worker interface.
type goWorker struct {
	runIn    Node
	tag      string
	pkgs     []string
	testFile string
	command  string
	probe    string
}

type buildCommand []string

// ood is true if we are older than our build in container.  We are also out of date
// if source has changed.
func (b *goWorker) ood(conf *Config, helper io.Helper, cli io.DockerCli,
	etcd io.EtcdClient, vbox io.VirtualBox) (time.Time, bool, error) {
	t, err := tagToTime(b.tag, cli)
	if err != nil {
		return time.Time{}, true, err
	}
	if t.IsZero() {
		fmt.Printf("[pickett] Building %s (tag not found)\n", b.tag)
		return time.Time{}, true, nil
	}
	if t.Before(b.runIn.Time()) {
		fmt.Printf("[pickett] Building %s (out of date with respect to '%s')\n", b.tag, b.runIn.Name())
		return time.Time{}, true, nil
	}

	//this is useful for either of the exit paths
	/*	insp, err := cli.DecodeInspect(b.tag)
		if err != nil {
			return time.Time{}, true, err
		}*/

	//This is here to support godeps.
	if b.testFile != "" {
		f, err := helper.OpenFileRelative(b.testFile)
		if err != nil {
			return time.Time{}, true, err
		}
		info, err := f.Stat()
		if err != nil {
			return time.Time{}, true, err
		}
		helper.Debug("mod time of %s is %v", b.testFile, info.ModTime())
		if t.Before(info.ModTime()) {
			return info.ModTime(), true, nil
		}
		fmt.Printf("[pickett] '%s' is up to date with respect to %s\n", b.tag, b.testFile)
		return t, false, nil
	}

	/// this case tests the go source code with a sequence of probes

	//we need to do this to test our source code for OOD
	sequence, err := b.formBuildCommand(conf, true, helper, vbox)
	if err != nil {
		return time.Time{}, true, err
	}
	for i, seq := range sequence {
		unpacked := []string(seq)
		if err := cli.CmdRun(false, unpacked...); err != nil {
			return time.Time{}, true, err
		}
		if !cli.EmptyOutput(true) {
			fmt.Printf("[pickett] Building %s (out of date with respect to source in %s)\n", b.tag, b.pkgs[i])
			cli.DumpErrOutput()
			return time.Time{}, true, nil
		}
	}

	fmt.Printf("[pickett] '%s' is up to date with respect to its source code.\n", b.tag)
	return t, false, nil
}

//formBuildCommand is a helper for forming the sequence of build-related commands to
//either probe for code out of date or build it.
func (b *goWorker) formBuildCommand(conf *Config, dontExecute bool, helper io.Helper,
	vbox io.VirtualBox) ([]buildCommand, error) {
	result := []buildCommand{}

	baseArgs := []string{}
	if conf.CodeVolume.Directory != "" {
		dir := helper.DirectoryRelative(conf.CodeVolume.Directory)
		mapped := dir
		if vbox.NeedPathTranslation() {
			var err error
			mapped, err = vbox.CodeVolumeToVboxPath(dir)
			if err != nil {
				return nil, err
			}
		}
		baseArgs = append(baseArgs, "-v", mapped+":"+conf.CodeVolume.MountedAt)
	}
	var baseCmd []string
	if dontExecute {
		baseCmd = strings.Split(strings.Trim(b.probe, " \n"), " ")
	} else {
		baseCmd = strings.Split(strings.Trim(b.command, " \n"), " ")
	}
	strCmd := strings.Trim(fmt.Sprint(baseCmd), "[]")
	for _, p := range b.pkgs {
		cmd := fmt.Sprintf("%s %s %s", b.runIn.Name(), strCmd, p)
		cmdArgs := append(baseArgs, strings.Split(cmd, " ")...)
		result = append(result, buildCommand(cmdArgs))
	}
	return result, nil
}

//build does the work of actually building go source code.
func (b *goWorker) build(conf *Config, helper io.Helper, cli io.DockerCli,
	etcd io.EtcdClient, vbox io.VirtualBox) (time.Time, error) {

	sequence, err := b.formBuildCommand(conf, false, helper, vbox)
	if err != nil {
		return time.Time{}, err
	}
	for _, seq := range sequence {
		unpacked := []string(seq)
		err := cli.CmdRun(true, unpacked...)
		if err != nil {
			cli.DumpErrOutput()
			return time.Time{}, err
		}
	}
	err = cli.CmdPs("-q", "-l")
	if err != nil {
		return time.Time{}, errors.New(fmt.Sprintf("failed trying to ps (%s): %v", b.tag, err))
	}
	id := cli.LastLineOfStdout()
	//command was ok, we need to tag it now
	err = cli.CmdCommit(id, b.tag)
	if err != nil {
		return time.Time{}, errors.New(fmt.Sprintf("failed trying to commit (%s): %v", b.tag, err))
	}
	insp, err := cli.DecodeInspect(b.tag)
	if err != nil {
		return time.Time{}, errors.New(fmt.Sprintf("failed trying to inspect (%s): %v", b.tag, err))
	}
	return insp.CreatedTime(), nil
}

func (g *goWorker) in() []Node {
	return []Node{
		g.runIn,
	}
}
