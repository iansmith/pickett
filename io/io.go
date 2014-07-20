package io

import (
	"bytes"
	"encoding/json"
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

//interestingPartsOfInspect is a utility for pulling things out of the json
//returned by docker server inspect function.
type interestingPartsOfInspect struct {
	Created time.Time
}

// IOHelper abstracts IO to make things easier to test.
type IOHelper interface {
	Debug(string, ...interface{})
	OpenDockerfileRelative(dir string) (io.Reader, error)
	DirectoryRelative(dir string) string
	Fatalf(string, ...interface{})
	CheckFatal(error, string, ...interface{})
	ConfigReader() io.Reader
	ConfigFile() string
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
	DecodeInspect(...string) (Inspected, error)
}

type Inspected interface {
	CreatedTime() time.Time
}

var (
	BAD_INSPECT_RESULT = errors.New("unable to understand result of docker inspect")
)

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
	//i.Debug("DirectoryRelative(%s)-->%s", dir, filepath.Clean(filepath.Join(i.pickettDir, dir)))
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

//NewDocker returns a connection to the docker server.  Note that this is usually
//only used by the driver program and most of the code inside picket assumes
//that a DockerCli is passed in from the outside.
func NewDocker(debug bool) (DockerCli, error) {
	if err := validateDockerHost(); err != nil {
		return nil, err
	}
	return newDockerCli(debug), nil
}

//validateDockerHost checks the environment for a sensible value for DOCKER_HOST.
func validateDockerHost() error {
	if os.Getenv("DOCKER_HOST") == "" {
		return NO_DOCKER_HOST
	}
	return nil
}

type dockerCli struct {
	out   *bytes.Buffer
	err   *bytes.Buffer
	cli   *docker.DockerCli
	debug bool
}

// newDockerCli builds a new docker interface and returns it. It
// assumes that the DOCKER_HOST env var has already been
// validated.
func newDockerCli(debug bool) DockerCli {
	result := &dockerCli{
		out:   new(bytes.Buffer),
		err:   new(bytes.Buffer),
		debug: debug,
	}

	//tee := io.MultiWriter(result.out, os.Stdout)
	parts := strings.Split(os.Getenv("DOCKER_HOST"), ":")
	result.cli = docker.NewDockerCli(nil, result.out, result.err,
		parts[0], parts[1], nil)
	if debug {
		fmt.Printf("[docker cmd] export DOCKER_HOST='%s'\n", os.Getenv("DOCKER_HOST"))
	}
	return result

}

func (d *dockerCli) reset() {
	d.out.Reset()
	d.err.Reset()
}

func (d *dockerCli) CmdRun(s ...string) error {
	return d.caller(d.cli.CmdRun, "run", s...)
}

func (d *dockerCli) CmdPs(s ...string) error {
	return d.caller(d.cli.CmdPs, "ps", s...)
}

func (d *dockerCli) CmdTag(s ...string) error {
	return d.caller(d.cli.CmdTag, "tag", s...)
}

func (d *dockerCli) CmdCommit(s ...string) error {
	return d.caller(d.cli.CmdCommit, "commit", s...)
}

//caller calls the clientAPI of docker on a function of your choice.  This handles the debugging
//output in a standard way so all docker commands and theere output look the same.
func (d *dockerCli) caller(fn func(s ...string) error, name string, s ...string) error {
	d.reset()
	if d.debug {
		fmt.Printf("[docker cmd] docker %s %s\n", name,
			strings.Trim(fmt.Sprint(s), "[]"))
	}
	err := fn(s...)
	if err != nil && d.debug {
		fmt.Printf("[docker result error!] %v\n", err)
		fmt.Printf("[err] %s\n", strings.Trim(d.err.String(), "\n"))
	} else if d.debug {
		if d.out.String() != "" {
			//fmt.Printf("string found '%s'\n", d.out.String())
			fmt.Printf("[docker result (%d lines)] %s\n",
				len(strings.Split(d.out.String(), "\n"))-1,
				d.LastLineOfStdout())
		}
	}
	return err
}

//DecodeInspect calls the nispect method of the docker CLI and then decodes the result.
//THis call will fail if the underlying inspect fails.
func (d *dockerCli) DecodeInspect(s ...string) (Inspected, error) {
	err := d.CmdInspect(s...)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(d.out)
	result := []interestingPartsOfInspect{}
	err = dec.Decode(&result)
	if err != nil {
		return nil, err
	}
	if len(result) != 1 {
		return nil, BAD_INSPECT_RESULT
	}
	return &result[0], nil

}

func (d *dockerCli) CmdInspect(s ...string) error {
	return d.caller(d.cli.CmdInspect, "inspect", s...)
}

func (d *dockerCli) CmdBuild(s ...string) error {
	return d.caller(d.cli.CmdBuild, "build", s...)
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
		fmt.Printf("docker result '%s'\n", s)
		panic("badly formed lines of output from docker command")
	}
	return lines[len(lines)-2]
}

func (d *dockerCli) Stderr() string {
	return d.err.String()
}

func (i *interestingPartsOfInspect) CreatedTime() time.Time {
	return time.Time(i.Created)
}
