package pickett

import (
	"fmt"
	"strings"
	"time"

	"github.com/igneous-systems/pickett/io"
)

// goBuilder has a dependecy between the object to build and the image
// that is used to build it.  This implements the builder interface.
type goBuilder struct {
	runIn      node
	repository string
	tagname    string
	pkgs       []string
	testFile   string
	command    string
	probe      string
}

func (g *goBuilder) tag() string {
	return g.repository + ":" + g.tagname
}

// ood is true if we are older than our build in container.  We are also out of date
// if source has changed.
func (g *goBuilder) ood(conf *Config) (time.Time, bool, error) {
	/// this case tests the go source code with a sequence of probes

	t, err := tagToTime(g.tag(), conf.cli)
	if err != nil {
		return time.Time{}, true, err
	}
	if t.IsZero() {
		flog.Infof("Building %s, tag not found.", g.tag())
		return time.Time{}, true, nil
	}
	if t.Before(g.runIn.time()) {
		flog.Infof("Building %s, out of date with respect to '%s'.", g.tag(), g.runIn.name())
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
		flog.Debugf("mod time of %s is %v", g.testFile, info.ModTime())
		if t.Before(info.ModTime()) {
			return info.ModTime(), true, nil
		}
		flog.Infof("'%s' is up to date with respect to %s.", g.tag(), g.testFile)
		return t, false, nil
	}

	/// this case tests the go source code with a sequence of probes

	//we need to do this to test our source code for OOD
	runConfig, sequence, err := g.formBuildCommand(conf, true)
	if err != nil {
		return time.Time{}, true, err
	}
	for i, seq := range sequence {
		if seq[0] == "sourceDirChecker" {
			sdc := NewSourceDirChecker(t)
			laterTime, err := sdc.Check(conf, seq[1])
			if err != nil {
				return time.Time{}, true, nil
			}
			if !laterTime.IsZero() {
				return laterTime, true, nil
			}
		} else {
			//fire for range
			buf, _, err := conf.cli.CmdRun(runConfig, seq...)
			if err != nil {
				return time.Time{}, true, err
			}
			if buf.Len() != 0 {
				flog.Infof("Building %s, out of date with respect to source in %s.", g.tag(), g.pkgs[i])
				return time.Time{}, true, nil
			}
		}
	}

	flog.Infof("'%s' is up to date with respect to its source code.", g.tag())
	return t, false, nil
}

type runCommand []string

//formBuildCommand is a helper for forming the sequence of build-related commands to
//either probe for code out of date or build it.
func (g *goBuilder) formBuildCommand(conf *Config, dontExecute bool) (*io.RunConfig, []runCommand, error) {

	attach := true
	waitOutput := true

	if dontExecute {
		attach = false
	}

	volumes, err := conf.codeVolumes()
	if err != nil {
		return nil, nil, err
	}

	resultConfig := &io.RunConfig{
		Attach:     attach,
		WaitOutput: waitOutput,
		Volumes:    volumes,
		Image:      g.runIn.name(),
	}

	var baseCmd []string
	if dontExecute {
		baseCmd = strings.Split(strings.Trim(g.probe, " \n"), " ")
	} else {
		baseCmd = strings.Split(strings.Trim(g.command, " \n"), " ")
	}
	sequence := []runCommand{}
	for _, p := range g.pkgs {
		rc := runCommand(append(baseCmd, p))
		sequence = append(sequence, rc)
	}

	return resultConfig, sequence, nil
}

//build does the work of actually building go source code.
func (g *goBuilder) build(conf *Config) (time.Time, error) {

	runConfig, sequence, err := g.formBuildCommand(conf, false)
	if err != nil {
		return time.Time{}, err
	}
	img := runConfig.Image

	for _, seq := range sequence {
		runConfig.Image = img
		_, contId, err := conf.cli.CmdRun(runConfig, seq...)
		if err != nil {
			return time.Time{}, err
		}
		//update the image
		img, err = conf.cli.CmdCommit(contId, nil)
		if err != nil {
			return time.Time{}, err
		}
	}

	//command was ok, we need to tag it now
	err = conf.cli.CmdTag(img, true, &io.TagInfo{g.repository, g.tagname})
	if err != nil {
		return time.Time{}, fmt.Errorf("failed trying to commit (%s): %v", g.tag(), err)
	}
	insp, err := conf.cli.InspectImage(g.tag())
	if err != nil {
		return time.Time{}, fmt.Errorf("failed trying to inspect (%s): %v", g.tag(), err)
	}
	return insp.CreatedTime(), nil
}

func (g *goBuilder) in() []node {
	return []node{
		g.runIn,
	}
}

//
// sourceDirChecker is a utility type for doing cehcks of a sequence of
// directories (and their subdirs).
//
type sourceDirChecker struct {
	target time.Time
}

func NewSourceDirChecker(t time.Time) *sourceDirChecker {
	return &sourceDirChecker{
		target: t,
	}
}

//Check that the path(relative to the config file) is up to date.
//Returns time's zero if everything is older than our target time. Returns the time
//if found something newer than target (there might be others).  This
//function checks subdirectories, so you should pass the root directory of
//the check you want to perform.
func (s *sourceDirChecker) Check(config *Config, path string) (time.Time, error) {
	flog.Infof("XXXX checking directory %s versus timestamp %v", path, s.target)
	t, err := config.helper.LastTimeInDirRelative(path)
	if err != nil {
		flog.Errorf("checking timestamp failed during sourceDirChecker: %v, %v", path, err)
		return time.Time{}, err
	}
	flog.Infof("XXXX got %v %v", t, t.After(s.target))

	if t.After(s.target) {
		return t, nil
	}
	return time.Time{}, nil
}
