package pickett

import (
	"fmt"
	"github.com/igneous-systems/pickett/io"
	"os"
	"path/filepath"
	"strings"
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
		fmt.Printf("[pickett] Building %s (out of date with respect to %s)\n", e.tag(), e.mergeWith.name)
		return time.Time{}, true, nil
	}

	//get the last change to a source  artifact
	last, sources, err := e.getSourceExtractions(conf)
	if err != nil {
		return time.Time{}, true, err
	}

	if t.Before(last) {
		fmt.Printf("[pickett] Building %s (out of date with respect to source artifact)\n", e.tag())
		return time.Time{}, true, nil
	}

	art := e.toCopyArtifacts()

	//
	// XXX It's not clear that this code is ever called.  To get this code to be called, you would
	// XXX have to have a container e.runIn.name that has an OLDER modification time than something
	// XXX that inside it.  In the normal case, where the tag time of container is at least as old
	// XXX as any artifact inside it, then the code above that checks for the timestamp of the runIn
	// XXX node would be invoked and we would not reach here.
	// XXX
	// XXX This code has been written for two "strange" cases that may never really occur.  One is when
	// XXX you have caching turned to make container build faster. This prevents the tag time from
	// XXX getting updated so perhaps it's possible you could have things inside the container that
	// XXX that are "newer" than the tag time.  Further, it might be possible that you built the
	// XXX underlying image on a different machine, with a different clock, than the one you are running
	// XXX on, and thus tagging on.  In this latter case it's clearly possible to get a tag time that
	// XXX is older than contents in the "inside" of the container.  What's not clear is whether or not
	// XXX you have ANY hope of running successfully in a situation this broken.

	inContLast, err := conf.cli.CmdLastModTime(sources, e.runIn.name, art)
	if err != nil {
		return time.Time{}, true, err
	}

	if t.Before(inContLast) {
		fmt.Printf("[pickett] Building %s (out of date with respect to container artifact)\n", e.tag())
		return time.Time{}, true, nil
	}

	fmt.Printf("[pickett] '%s' is up to date\n", e.tag())
	return t, false, nil
}

func lastTimeInADirTree(path string, bestSoFar time.Time) (time.Time, error) {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}, err
	}
	if !info.IsDir() {
		if info.ModTime().After(bestSoFar) {
			return info.ModTime(), nil
		}
		return bestSoFar, nil
	}
	fp, err := os.Open(path)
	if err != nil {
		return time.Time{}, err
	}
	names, err := fp.Readdirnames(0)
	if err != nil {
		return time.Time{}, err
	}
	best := bestSoFar
	for _, name := range names {
		child := filepath.Join(path, name)
		t, err := lastTimeInADirTree(child, best)
		if err != nil {
			return time.Time{}, err
		}
		if t.After(best) {
			best = t
		}
	}
	return best, nil
}

//This function is here to walk around on the known artifacts looking for ones that happen to be "inside"
//the source directories.  Things that are have to handled specially by various parts of the extraction.
func (e *extractionBuilder) getSourceExtractions(conf *Config) (time.Time, map[string]string, error) {

	//note that this is NOT path translated for the virtual machine!!
	volumes := make(map[string]string)
	for _, cv := range conf.CodeVolumes {
		dir := conf.helper.DirectoryRelative(cv.Directory)
		volumes[dir] = cv.MountedAt
	}

	//this is keyed by the source of the artifacts
	realPathSource := make(map[string]string)

	// we have to detect things in the mounted volumes
	for _, a := range e.artifacts {
		candidateIn := filepath.Clean(a.BuiltPath)
		candidateOut := filepath.Clean(a.DestinationDir)

		for k, v := range volumes {
			mountPoint := filepath.Clean(v)
			if strings.HasPrefix(candidateIn, mountPoint) {
				sourcePath := k + candidateIn[len(mountPoint):]
				realPathSource[a.BuiltPath] = sourcePath
			}
			if strings.HasPrefix(candidateOut, mountPoint) {
				return time.Time{}, nil, fmt.Errorf("should not be copying things into the source directories for extraction: %s",
					a.DestinationDir)
			}
		}
	}

	best := time.Time{}
	for _, p := range realPathSource {
		t, err := lastTimeInADirTree(p, best)
		if err != nil {
			return time.Time{}, nil, err
		}
		if t.After(best) {
			best = t
		}
	}
	return best, realPathSource, nil
}

func (e *extractionBuilder) toCopyArtifacts() []*io.CopyArtifact {
	art := []*io.CopyArtifact{}
	for _, a := range e.artifacts {
		cp := &io.CopyArtifact{
			SourcePath:     a.BuiltPath,
			DestinationDir: a.DestinationDir,
		}
		art = append(art, cp)
	}
	return art
}

//build does the work of coping data from the source image (runIn) and then
//adding it to the merge image (mergeWith)
func (e *extractionBuilder) build(conf *Config) (time.Time, error) {

	var err error

	_, realPathSource, err := e.getSourceExtractions(conf)
	if err != nil {
		return time.Time{}, err
	}

	art := e.toCopyArtifacts()

	err = conf.cli.CmdCopy(realPathSource, e.runIn.name, e.mergeWith.name, art, e.tag())
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
