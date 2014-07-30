package pickett

import (
	"fmt"
	"strings"
	"time"

	docker_utils "github.com/docker/docker/utils"

	"github.com/igneous-systems/pickett/io"
)

const (
	SUCCESS_MAGIC = "Successfully built "
)

//containerBuilder represents a node in the dependency graph that understands
//about how to build a docker image.  This implements the worker interface.
type containerBuilder struct {
	tag     string
	dir     string
	imgTime time.Time
	dirTime time.Time
	inEdges []node
}

//helper func to look up the timestamp for a given tag in docker. The input
//can be a tag or an id.
func tagToTime(tag string, cli io.DockerCli) (time.Time, error) {
	interesting, err := cli.DecodeInspect(tag)
	if err != nil {
		statusErr, ok := err.(*docker_utils.StatusError)
		if !ok {
			return time.Time{}, err
		}
		//XXX is this right?
		if statusErr.StatusCode == 1 {
			return time.Time{}, nil
		}
		return time.Time{}, err
	}
	return interesting.CreatedTime(), nil
}

//setTimestampOnImage sets the timestamp that docker has registered for this image.
func (d *containerBuilder) setTimestampOnImage(helper io.Helper, cli io.DockerCli) error {
	t, err := tagToTime(d.tag, cli)
	if err != nil {
		return err
	}
	if t.IsZero() {
		helper.Debug("setTimestampOnImage %s: doesn't exist", d.tag)
	} else {
		helper.Debug("setTimestampOnImage %s to be %v", d.tag, t)
	}
	d.imgTime = t
	return nil
}

//setLastTimeOnDirectoryEntry looks at the directory in this worker and returns the latest
//modification time found on a file in that directory.
func (d *containerBuilder) setLastTimeOnDirectoryEntry(helper io.Helper) error {
	last, err := helper.LastTimeInDirRelative(d.dir)
	if err != nil {
		return err
	}
	helper.Debug("setLastTimeOnDirectoryEntry(%s) to be %v", d.dir, last)
	d.dirTime = last
	return nil
}

//ood compares a docker image time to the latest timestamp in the directory
//that holds the dockerfile.  Note that an image that is unknown is not out of date
//with respect to an empty directory (time stamps are equal).  This returns the image
//time if we say false or "this is not ood".
func (d *containerBuilder) ood(conf *Config) (time.Time, bool, error) {
	if err := d.setLastTimeOnDirectoryEntry(conf.helper); err != nil {
		return time.Time{}, true, err
	}

	if err := d.setTimestampOnImage(conf.helper, conf.cli); err != nil {
		return time.Time{}, true, err
	}

	if d.dirTime.After(d.imgTime) {
		fmt.Printf("[pickett] '%s' needs to be rebuilt, source directory %s is newer.\n", d.tag, d.dir)
		return time.Time{}, true, nil
	}

	fmt.Printf("[pickett] '%s' is up to date with respect to its build directory.\n", d.tag)
	return d.imgTime, false, nil
}

//build constructs a new image based on a directory that has a dockerfile. It
//calls the docker server to actually perform the build.
func (d *containerBuilder) build(config *Config) (time.Time, error) {
	config.helper.Debug("Building '%s'...", d.tag)

	buildOpts := append(config.DockerBuildOptions, config.helper.DirectoryRelative(d.dir))
	err := config.cli.CmdBuild(true, buildOpts...)
	if err != nil {
		return time.Time{}, err
	}

	last_line := config.cli.LastLineOfStdout()
	if !strings.HasPrefix(last_line, SUCCESS_MAGIC) {
		panic("can't understand the success message from docker!")
	}
	id := last_line[len(SUCCESS_MAGIC):]
	err = config.cli.CmdTag("-f", id, d.tag)
	if err != nil {
		return time.Time{}, err
	}
	//read it back from docker to get the new time
	d.setTimestampOnImage(config.helper, config.cli)
	return d.imgTime, nil
}

func (s *containerBuilder) in() []node {
	return s.inEdges
}
