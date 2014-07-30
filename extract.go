package pickett

import (
	"fmt"
	"github.com/igneous-systems/pickett/io"
	"time"
)

// extractionBuilder implements the builder interface so it can be part of a node.
// extractions pull things from their runIn image or node and merge them into another
// node or image.  typically, they are used to get build artifacts out of a conatiner
// that has the build tools.
type extractionBuilder struct {
	repository string
	tagname    string
	runIn      nodeOrName
	mergeWith  nodeOrName
	artifacts  []*Artifact
}

func (e *extractionBuilder) tag() string {
	return e.repository + ":" + e.tagname
}

// IsOutOfDate returns true if the tag that we are trying to produce is
// before the tag of the image we depend on.
func (e *extractionBuilder) ood(conf *Config) (time.Time, bool, error) {
	t, err := tagToTime(e.tag(), conf.cli)
	if err != nil {
		return time.Time{}, true, err
	}
	if t.IsZero() {
		fmt.Printf("[pickett] Building %s (tag not found)\n", e.tag())
		return time.Time{}, true, nil
	}
	if e.runIn.isNode && t.Before(e.runIn.node.time()) {
		fmt.Printf("[pickett] Building %s (out of date with respect to %s)\n", e.tag(), e.runIn.name)
		return time.Time{}, true, nil
	}
	if e.mergeWith.isNode && t.Before(e.mergeWith.node.time()) {
		fmt.Printf("[pickett] Building %s (out of date with respect to %s)\n", e.tag(), e.runIn.name)
		return time.Time{}, true, nil
	}
	fmt.Printf("[pickett] '%s' is up to date\n", e.tag())
	return t, false, nil
}

//build does the work of coping data from the source image (runIn) and then
//adding it to the merge image (mergeWith)
func (e *extractionBuilder) build(conf *Config) (time.Time, error) {
	if conf.CodeVolume.MountedAt == "" {
		return time.Time{}, fmt.Errorf("not clever enough to copy artifacts that are not on a code volume!")
	}
	dir := conf.helper.DirectoryRelative(conf.CodeVolume.Directory)
	path := dir
	var err error
	if conf.vbox.NeedPathTranslation() {
		path, err = conf.vbox.CodeVolumeToVboxPath(dir)
		if err != nil {
			return time.Time{}, err
		}
	}
	//initialize with the value from the config file
	curr := e.mergeWith.name
	volumes := make(map[string]string)
	volumes[path] = conf.CodeVolume.MountedAt

	for _, a := range e.artifacts {
		runConfig := &io.RunConfig{
			Volumes:    volumes,
			Image:      curr,
			Attach:     false,
			WaitOutput: true,
		}
		runCmd := []string{"cp"}
		if a.IsDirectory { // If artifact is a directory, recursively copy it
			runCmd = []string{"cp", "-rf"}
		}
		runCmd = append(runCmd, a.BuiltPath, a.DestinationPath)
		conf.helper.Debug("copying artifact with cp: %s -> %s.", a.BuiltPath, a.DestinationPath)
		buf, id, err := conf.cli.CmdRun(runConfig, runCmd...)
		if err != nil {
			return time.Time{}, err
		}
		insp, err := conf.cli.InspectContainer(id)
		if err != nil {
			return time.Time{}, err
		}
		if insp.ExitStatus() != 0 {
			return time.Time{}, fmt.Errorf("%s", buf.String())
		}

		curr, err = conf.cli.CmdCommit(id, nil)
		if err != nil {
			return time.Time{}, err
		}
	}
	err = conf.cli.CmdTag(curr, true, &io.TagInfo{Repository: e.repository, Tag: e.tagname})
	if err != nil {
		return time.Time{}, err
	}
	insp, err := conf.cli.InspectImage(e.tag())
	if err != nil {
		return time.Time{}, err
	}
	conf.helper.Debug("done copying, time for %s is %v", e.tag(), insp.CreatedTime())
	return insp.CreatedTime(), nil
}

//in returns the inbound edges.  This is not as simple as it would appear
//beacuse the runIn and mergeWith attributes can be a just a tag (image name) not necessarily
//a node.
func (e *extractionBuilder) in() []node {
	result := []node{}
	if e.runIn.isNode {
		result = append(result, e.runIn.node)
	}
	if e.mergeWith.isNode {
		result = append(result, e.mergeWith.node)
	}
	return result
}
