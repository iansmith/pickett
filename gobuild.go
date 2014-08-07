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
	fmt.Printf("xxxx ENTER OOD\n")

	t, err := tagToTime(g.tag(), conf.cli)
	if err != nil {
		return time.Time{}, true, err
	}
	if t.IsZero() {
		fmt.Printf("[pickett] Building %s, tag not found.\n", g.tag())
		return time.Time{}, true, nil
	}
	if t.Before(g.runIn.time()) {
		fmt.Printf("[pickett] Building %s, out of date with respect to '%s'.\n", g.tag(), g.runIn.name())
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
		fmt.Printf("[pickett] '%s' is up to date with respect to %s\n.", g.tag(), g.testFile)
		return t, false, nil
	}

	/// this case tests the go source code with a sequence of probes
	fmt.Printf("xxxx FORM BUILD COMMOND\n")

	//we need to do this to test our source code for OOD
	runConfig, sequence, err := g.formBuildCommand(conf, true)
	if err != nil {
		return time.Time{}, true, err
	}
	for i, seq := range sequence {
		fmt.Printf("xxxx %+v %+v\n", runConfig, sequence)
		//fire for range
		buf, _, err := conf.cli.CmdRun(runConfig, seq...)
		if err != nil {
			return time.Time{}, true, err
		}
		if buf.Len() != 0 {
			fmt.Printf("[pickett] Building %s, out of date with respect to source in %s.\n", g.tag(), g.pkgs[i])
			return time.Time{}, true, nil
		}
	}

	fmt.Printf("[pickett] '%s' is up to date with respect to its source code.\n", g.tag())
	return t, false, nil
}

type runCommand []string

//formBuildCommand is a helper for forming the sequence of build-related commands to
//either probe for code out of date or build it.
func (g *goBuilder) formBuildCommand(conf *Config, dontExecute bool) (*io.RunConfig, []runCommand, error) {

	attach := true
	waitOutput := false

	if dontExecute {
		attach = false
		waitOutput = true
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
