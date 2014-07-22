package pickett

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// goBuilder has a dependecy between the object to build and the image
// that is used to build it.  This implements the builder interface.
type goBuilder struct {
	runIn    node
	tag      string
	pkgs     []string
	testFile string
	command  string
	probe    string
}

type buildCommand []string

// ood is true if we are older than our build in container.  We are also out of date
// if source has changed.
func (g *goBuilder) ood(conf *Config) (time.Time, bool, error) {
	t, err := tagToTime(g.tag, conf.cli)
	if err != nil {
		return time.Time{}, true, err
	}
	if t.IsZero() {
		fmt.Printf("[pickett] Building %s, tag not found.\n", g.tag)
		return time.Time{}, true, nil
	}
	if t.Before(g.runIn.time()) {
		fmt.Printf("[pickett] Building %s, out of date with respect to '%s'.\n", g.tag, g.runIn.name())
		return time.Time{}, true, nil
	}

	//This is here to support godeps.
	if g.testFile != "" {
		f, err := conf.helper.OpenFileRelative(g.testFile)
		if err != nil {
			return time.Time{}, true, err
		}
		info, err := f.Stat()
		if err != nil {
			return time.Time{}, true, err
		}
		conf.helper.Debug("mod time of %s is %v", g.testFile, info.ModTime())
		if t.Before(info.ModTime()) {
			return info.ModTime(), true, nil
		}
		fmt.Printf("[pickett] '%s' is up to date with respect to %s\n.", g.tag, g.testFile)
		return t, false, nil
	}

	/// this case tests the go source code with a sequence of probes

	//we need to do this to test our source code for OOD
	sequence, err := g.formBuildCommand(conf, true)
	if err != nil {
		return time.Time{}, true, err
	}
	for i, seq := range sequence {
		unpacked := []string(seq)
		if err := conf.cli.CmdRun(false, unpacked...); err != nil {
			return time.Time{}, true, err
		}
		if !conf.cli.EmptyOutput(true) {
			fmt.Printf("[pickett] Building %s, out of date with respect to source in %s.\n", g.tag, g.pkgs[i])
			return time.Time{}, true, nil
		}
	}

	fmt.Printf("[pickett] '%s' is up to date with respect to its source code.\n", g.tag)
	return t, false, nil
}

//formBuildCommand is a helper for forming the sequence of build-related commands to
//either probe for code out of date or build it.
func (g *goBuilder) formBuildCommand(conf *Config, dontExecute bool) ([]buildCommand, error) {
	result := []buildCommand{}

	baseArgs := []string{}
	if conf.CodeVolume.Directory != "" {
		dir := conf.helper.DirectoryRelative(conf.CodeVolume.Directory)
		mapped := dir
		if conf.vbox.NeedPathTranslation() {
			var err error
			mapped, err = conf.vbox.CodeVolumeToVboxPath(dir)
			if err != nil {
				return nil, err
			}
		}
		baseArgs = append(baseArgs, "-v", mapped+":"+conf.CodeVolume.MountedAt)
	}
	var baseCmd []string
	if dontExecute {
		baseCmd = strings.Split(strings.Trim(g.probe, " \n"), " ")
	} else {
		baseCmd = strings.Split(strings.Trim(g.command, " \n"), " ")
	}
	strCmd := strings.Trim(fmt.Sprint(baseCmd), "[]")
	for _, p := range g.pkgs {
		cmd := fmt.Sprintf("%s %s %s", g.runIn.name(), strCmd, p)
		cmdArgs := append(baseArgs, strings.Split(cmd, " ")...)
		result = append(result, buildCommand(cmdArgs))
	}
	return result, nil
}

//build does the work of actually building go source code.
func (g *goBuilder) build(conf *Config) (time.Time, error) {

	sequence, err := g.formBuildCommand(conf, false)
	if err != nil {
		return time.Time{}, err
	}
	for _, seq := range sequence {
		unpacked := []string(seq)
		err := conf.cli.CmdRun(true, unpacked...)
		if err != nil {
			conf.cli.DumpErrOutput()
			return time.Time{}, err
		}
	}
	err = conf.cli.CmdPs("-q", "-l")
	if err != nil {
		return time.Time{}, errors.New(fmt.Sprintf("failed trying to ps (%s): %v", g.tag, err))
	}
	id := conf.cli.LastLineOfStdout()
	//command was ok, we need to tag it now
	err = conf.cli.CmdCommit(id, g.tag)
	if err != nil {
		return time.Time{}, errors.New(fmt.Sprintf("failed trying to commit (%s): %v", g.tag, err))
	}
	insp, err := conf.cli.DecodeInspect(g.tag)
	if err != nil {
		return time.Time{}, errors.New(fmt.Sprintf("failed trying to inspect (%s): %v", g.tag, err))
	}
	return insp.CreatedTime(), nil
}

func (g *goBuilder) in() []node {
	return []node{
		g.runIn,
	}
}
