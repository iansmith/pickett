package pickett

import (
	"github.com/igneous-systems/pickett/io"
	"time"
)

//containerBuilder represents a node in the dependency graph that understands
//about how to build a docker image.  This implements the worker interface.
type containerBuilder struct {
	repository string
	tagname    string
	dir        string
	imgTime    time.Time
	dirTime    time.Time
	inEdges    []node
}

func (c *containerBuilder) tag() string {
	return c.repository + ":" + c.tagname
}

//helper func to look up the timestamp for a given tag in docker. The input
//can be a tag or an id.
func tagToTime(tag string, cli io.DockerCli) (time.Time, error) {
	interesting, err := cli.InspectImage(tag)
	if err != nil {
		//flog.Errorf("xxx is it ok to ignore this error on inspect %s?: %v", tag, err)
		return time.Time{}, nil
	}
	return interesting.CreatedTime(), nil
}

//setTimestampOnImage sets the timestamp that docker has registered for this image.
func (d *containerBuilder) setTimestampOnImage(helper io.Helper, cli io.DockerCli) error {
	t, err := tagToTime(d.tag(), cli)
	if err != nil {
		return err
	}
	if t.IsZero() {
		flog.Debugf("setTimestampOnImage %s: doesn't exist", d.tag())
	} else {
		flog.Debugf("setTimestampOnImage %s to be %v", d.tag(), t)
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
	flog.Debugf("setLastTimeOnDirectoryEntry(%s) to be %v", d.dir, last)
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
		flog.Infof("'%s' needs to be rebuilt, source directory %s is newer.", d.tag(), d.dir)
		return time.Time{}, true, nil
	}

	flog.Infof("'%s' is up to date with respect to its build directory.", d.tag())
	return d.imgTime, false, nil
}

//build constructs a new image based on a directory that has a dockerfile. It
//calls the docker server to actually perform the build.
func (d *containerBuilder) build(config *Config) (time.Time, error) {

	opts := &io.BuildConfig{
		NoCache:                  config.DockerBuildOptions.DontUseCache,
		RemoveTemporaryContainer: config.DockerBuildOptions.RemoveContainer,
	}
	dirName := config.helper.DirectoryRelative(d.dir)
	flog.Infof("Building tarball in %s", d.dir)

	//now can send it to the server
	err := config.cli.CmdBuild(opts, dirName, d.tag())
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
