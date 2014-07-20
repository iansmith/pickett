package pickett

import (
	"fmt"
	"strings"
	"time"

	docker_utils "github.com/dotcloud/docker/utils"

	"github.com/igneoussystems/pickett/io"
)

//DockerSourceNode represents a node in the dependency graph that understands
//about how to build a docker image.  This implements the Node interface.
type DockerSourceNode struct {
	name    string
	dir     string
	imgTime time.Time
	dirTime time.Time
	in      []Node
	out     []Node
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
	return time.Time(interesting.Created()), nil
}

//setTimestampOnImage sets the timestamp that docker has registered for a given image
//name.  The name given should be unique.  If no image with the name can be found,
//the zero value of time.Time is returned, not an error.  Any error is likely fatal.
func (d *DockerSourceNode) setTimestampOnImage(helper io.IOHelper, cli io.DockerCli) error {
	t, err := tagToTime(d.name, cli)
	if err != nil {
		return err
	}
	if t.IsZero() {
		helper.Debug("setTimestampOnImage %s: doesn't exist", d.name)
	} else {
		helper.Debug("setTimestampOnImage %s to be %v", d.name, t)
	}
	d.imgTime = t
	return nil
}

//setLastTimeOnDirectoryEntry looks at the directory in the node and returns the latest
//modification time found on a file in that directory.
func (d *DockerSourceNode) setLastTimeOnDirectoryEntry(helper io.IOHelper) error {
	last, err := helper.LastTimeInDirRelative(d.dir)
	if err != nil {
		return err
	}
	helper.Debug("setLastTimeOnDirectoryEntry(%s) to be %v", d.dir, last)
	d.dirTime = last
	return nil
}

//IsOutOfDateCompares a docker image time to the latest timestamp in the directory
//that holds the dockerfile.  Note that an image that is unknown is not out of date
//with respect to an empty directory (time stamps are equal).
func (d *DockerSourceNode) IsOutOfDate(conf *Config, helper io.IOHelper, cli io.DockerCli) (bool, error) {
	if err := d.setLastTimeOnDirectoryEntry(helper); err != nil {
		return false, err
	}

	if err := d.setTimestampOnImage(helper, cli); err != nil {
		return false, err
	}

	if d.dirTime.After(d.imgTime) {
		fmt.Printf("[pickett] %s needs to be rebuilt (source directory %s is newer)\n", d.name, d.dir)
		return true, nil
	}

	for _, in := range d.in {
		if d.imgTime.Before(in.Time()) {
			fmt.Printf("[pickett] %s needs to be rebuilt (%s is newer)\n",
				d.name, in.Name())
			return true, nil
		}
	}
	return false, nil
}

//Time returns the most recently built time for this node type.
func (d *DockerSourceNode) Time() time.Time {
	return d.imgTime
}

//BringInboundUpToDate walks all the inbound edges and calls Build() on each one.
//This process is recursive.
func (d *DockerSourceNode) BringInboundUpToDate(config *Config, helper io.IOHelper, cli io.DockerCli) error {
	for _, in := range d.in {
		if err := in.Build(config, helper, cli); err != nil {
			return err
		}
	}
	return nil
}

//AddOut adds an outgoing edge.
func (s *DockerSourceNode) AddOut(n Node) {
	s.out = append(s.out, n)
}

//Name prints the name of this node for a human to consume
func (s *DockerSourceNode) Name() string {
	return s.name
}

//Build constructs a new image based on a directory that has a dockerfile. It
//calls the docker server to actuallyli perform the build.
func (d *DockerSourceNode) Build(config *Config, helper io.IOHelper, cli io.DockerCli) error {
	helper.Debug("Building '%s'...", d.Name())
	err := d.BringInboundUpToDate(config, helper, cli)
	if err != nil {
		return err
	}

	b, err := d.IsOutOfDate(config, helper, cli)
	if err != nil {
		return err
	}
	if !b {
		fmt.Printf("[pickett] %s is up to date.\n", d.name)
		return nil
	}

	buildOpts := append(config.DockerBuildOptions, helper.DirectoryRelative(d.dir))
	err = cli.CmdBuild(buildOpts...)
	if err != nil {
		return err
	}

	last_line := cli.LastLineOfStdout()
	magic := "Successfully built "
	if !strings.HasPrefix(last_line, magic) {
		panic("can't understand the success message from docker!")
	}
	id := last_line[len(magic):]
	err = cli.CmdTag("-f", id, d.name)
	if err != nil {
		return err
	}
	//read it back from docker to get the new time
	d.setTimestampOnImage(helper, cli)
	return nil
}

//IsSink is true if this node has no edges from it.
func (d *DockerSourceNode) IsSink() bool {
	return len(d.out) == 0
}
