package pickett

import (
	"fmt"
	"strings"
	"time"

	"github.com/igneous-systems/pickett/io"
)

// goworker has a dependecy between the object to build and the image
// that is used to build it.  This implements the worker interface.
type goWorker struct {
	runIn Node
	tag   string
	pkgs  []string
	test  bool
}

type buildCommand []string

// ood is true if we are older than our build in container.  We are also out of date
// if source has changed.
func (b *goWorker) ood(conf *Config, helper io.IOHelper, cli io.DockerCli) (time.Time, bool, error) {
	t, err := tagToTime(b.tag, cli)
	if err != nil {
		return time.Time{}, true, err
	}
	if t.IsZero() {
		fmt.Printf("[pickett] Building %s (tag not found)\n", b.tag)
		return time.Time{}, true, nil
	}
	if t.Before(b.runIn.Time()) {
		fmt.Printf("[pickett] Building %s (out of date with respect to %s)\n", b.tag, b.runIn.Name())
		return time.Time{}, true, nil
	}
	//we need to do this to test our source code for OOD
	sequence := b.formBuildCommand(conf, true, helper)
	for i, seq := range sequence {
		unpacked := []string(seq)
		if err := cli.CmdRun(unpacked...); err != nil {
			return time.Time{}, true, err
		}
		if !cli.EmptyOutput() {
			fmt.Printf("[pickett] Building %s (out of date with respect to source in %s)\n", b.tag, b.pkgs[i])
			return time.Time{}, true, nil
		}
	}
	//if we reach here, we tried all the code and found it up to date
	insp, err := cli.DecodeInspect(b.tag)
	if err != nil {
		return time.Time{}, true, err
	}
	fmt.Printf("[pickett] '%s' is up to date with respect to its source code.\n", b.tag)
	return insp.CreatedTime(), false, nil
}

func (b *goWorker) formBuildCommand(conf *Config, dontExecute bool, helper io.IOHelper) []buildCommand {
	result := []buildCommand{}

	baseArgs := []string{}
	if conf.CodeVolume.Directory != "" {
		dir := helper.DirectoryRelative(conf.CodeVolume.Directory)
		baseArgs = append(baseArgs, "-v", dir+":"+conf.CodeVolume.MountedAt)
	}
	baseCmd := "install"
	if b.test {
		baseCmd = "test"
	}
	if dontExecute {
		baseCmd = baseCmd + " -n"
	}

	for _, p := range b.pkgs {
		cmd := fmt.Sprintf("%s go %s %s", b.runIn.Name(), baseCmd, p)
		cmdArgs := append(baseArgs, strings.Split(cmd, " ")...)
		result = append(result, buildCommand(cmdArgs))
	}
	return result
}

func (b *goWorker) build(conf *Config, helper io.IOHelper, cli io.DockerCli) (time.Time, error) {

	//we need to do this to test our source code for OOD
	sequence := b.formBuildCommand(conf, false, helper)
	for _, seq := range sequence {
		unpacked := []string(seq)
		err := cli.CmdRun(unpacked...)
		if err != nil {
			return time.Time{}, err
		}
	}
	err := cli.CmdPs("-q", "-l")
	if err != nil {
		return time.Time{}, fmt.Errorf("failed trying to ps (%s): %v", b.tag, err)
	}
	id := cli.LastLineOfStdout()
	//command was ok, we need to tag it now
	err = cli.CmdCommit(id, b.tag)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed trying to commit (%s): %v", b.tag, err)
	}
	insp, err := cli.DecodeInspect(b.tag)
	if err != nil {
		return time.Time{}, fmt.Errorf("failed trying to inspect (%s): %v", b.tag, err)
	}
	return insp.CreatedTime(), nil
}

func (g *goWorker) in() []Node {
	return []Node{
		g.runIn,
	}
}
