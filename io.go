package pickett

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	docker "github.com/dotcloud/docker/api/client"
)

var (
	NO_DOCKER_HOST         = errors.New("no DOCKER_HOST found in environment, please set it")
	BAD_DOCKER_HOST_FORMAT = errors.New("DOCKER_HOST found, but should be host:port")
)

// IOHelper abstracts IO to make things easier to test.
type IOHelper interface {
	Debug(string, ...interface{})
	OpenDockerfileRelative(dir string) (io.Reader, error)
	DirectoryRelative(dir string) string
	Fatalf(string, ...interface{})
	CheckFatal(error, string, ...interface{})
	ConfigReader() io.Reader
	ConfigFile() string
	Docker() (DockerCli, error)
	LastTimeInDirRelative(dir string) (time.Time, error)
}

type DockerCli interface {
	CmdRun(...string) error
	CmdPs(...string) error
	CmdTag(...string) error
	CmdCommit(...string) error
	CmdInspect(...string) error
	CmdBuild(...string) error
	Stdout() string
	LastLineOfStdout() string
	Stderr() string
}

// NewIOHelper creates an implementation of the IOHelper that runs against
// a real filesystem.  This is used when running normally.  The parameter
// filepath should be a path to a Pickett.json file.  This should be a
// fully qualified path (starting with /).
func NewIOHelper(fullPath string, debug bool) (IOHelper, error) {
	_, err := os.Open(fullPath)
	if err != nil {
		return nil, err
	}
	return &ioHelper{
		pickettDir: filepath.Dir(fullPath),
		confFile:   fullPath,
		debug:      debug,
	}, nil
}

// ioHelper is the normal impl of IOHelper.
type ioHelper struct {
	pickettDir string
	confFile   string
	debug      bool
}

//Debug prints a useful message to pickett developers.
func (i *ioHelper) Debug(fmtSpec string, p ...interface{}) {
	if i.debug {
		fmt.Printf("[debug] "+fmtSpec+"\n", p...)
	}
}

// OpenDockerfileRelative returns a reader connected to the Dockerfile
// requsted in dir (relative to the Pickett.json) or an error.
func (i *ioHelper) OpenDockerfileRelative(dir string) (io.Reader, error) {
	i.Debug("OpenDockerfileRelative(%s)-->%s", dir, filepath.Join(i.DirectoryRelative(dir), "Dockerfile"))
	return os.Open(filepath.Join(i.DirectoryRelative(dir), "Dockerfile"))
}

// Fatalf prints out a message and exits the program.
func (i *ioHelper) Fatalf(fmtSpec string, params ...interface{}) {
	fmt.Fprintf(os.Stderr, "[pickett] "+fmtSpec+"\n", params...)
	os.Exit(1)
}

// CheckFatal tests if the error is nil and if so, it calls the Fatalf
// function with the arguments.
func (i *ioHelper) CheckFatal(err error, fmtSpec string, params ...interface{}) {
	if err != nil {
		i.Fatalf(fmtSpec, append(params, err)...)
	}
}

// Return the true directory of a given directory that is relative to the
// pickett config file.
func (i *ioHelper) DirectoryRelative(dir string) string {
	i.Debug("DirectoryRelative(%s)-->%s", dir, filepath.Clean(filepath.Join(i.pickettDir, dir)))
	return filepath.Clean(filepath.Join(i.pickettDir, dir))
}

// Return a reader hooked to the configuration file we were initial
func (i *ioHelper) ConfigReader() io.Reader {
	i.Debug("ConfigReader trying to read %s", i.confFile)
	rd, err := os.Open(i.confFile)
	//we checked this on creation, so should not fail
	if err != nil {
		i.Fatalf("configuration file changed out from under us: %s : %v",
			i.confFile, err)
	}
	return rd
}

// ConfigFile returns the name of the original configuration file
// used to construct this object.
func (i *ioHelper) ConfigFile() string {
	return i.confFile
}

//LastTimeInDirRelative looks at the directory, relative to the configuration
//file and returns the latest modification time found on a file in that directory. Child directories are not
//neither searched nor their timestamps examined.  If there are no entries in the
//directory, then the latest time is defined to be the zero value of time.Time.
func (i *ioHelper) LastTimeInDirRelative(dir string) (time.Time, error) {
	i.Debug("LastTimeInDirRelative(%s)--> %s", dir, i.DirectoryRelative(dir))
	info, err := ioutil.ReadDir(i.DirectoryRelative(dir))
	if err != nil {
		return time.Time{}, err
	}
	var last time.Time
	for _, entry := range info {
		if entry.IsDir() {
			continue
		}
		if entry.ModTime().After(last) {
			last = entry.ModTime()
		}
	}
	return last, nil
}

func (*ioHelper) Docker() (DockerCli, error) {
	if err := validateDockerHost(); err != nil {
		return nil, err
	}
	return newDockerCli(), nil
}

//validateDockerHost checks the environment for a sensible value for DOCKER_HOST.
func validateDockerHost() error {
	if os.Getenv("DOCKER_HOST") == "" {
		return NO_DOCKER_HOST
	}
	if len(strings.Split(os.Getenv("DOCKER_HOST"), ":")) != 2 {
		return BAD_DOCKER_HOST_FORMAT
	}
	return nil
}

type dockerCli struct {
	out *bytes.Buffer
	err *bytes.Buffer
	cli *docker.DockerCli
}

// newDockerCli builds a new docker interface and returns it. It
// assumes that the DOCKER_HOST env var has already been
// validated.
func newDockerCli() DockerCli {
	result := &dockerCli{
		out: new(bytes.Buffer),
		err: new(bytes.Buffer),
	}
	tee := io.MultiWriter(result.out, os.Stdout)
	result.cli = docker.NewDockerCli(nil, tee, result.err,
		"tcp", os.Getenv("DOCKER_HOST"), nil)
	return result

}

func (d *dockerCli) reset() {
	d.out.Reset()
	d.err.Reset()
}

func (d *dockerCli) CmdRun(s ...string) error {
	d.reset()
	return d.cli.CmdRun(s...)
}

func (d *dockerCli) CmdPs(s ...string) error {
	d.reset()
	return d.cli.CmdPs(s...)
}

func (d *dockerCli) CmdTag(s ...string) error {
	d.reset()
	return d.cli.CmdTag(s...)
}

func (d *dockerCli) CmdCommit(s ...string) error {
	d.reset()
	return d.cli.CmdCommit(s...)
}

func (d *dockerCli) CmdInspect(s ...string) error {
	d.reset()
	return d.cli.CmdInspect(s...)
}

func (d *dockerCli) CmdBuild(s ...string) error {
	d.reset()
	return d.cli.CmdBuild(s...)
}

func (d *dockerCli) Stdout() string {
	return d.out.String()
}

func (d *dockerCli) LastLineOfStdout() string {
	s := d.out.String()
	lines := strings.Split(s, "\n")
	//there is a terminating \n on the last line, need to subtract 2 to get
	//the last element of this slice
	if len(lines) < 2 {
		panic("badly formed lines of output from docker command")
	}
	return lines[len(lines)-2]
}

func (d *dockerCli) Stderr() string {
	return d.err.String()
}
