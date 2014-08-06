package io

import (
	"archive/tar"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/fsouza/go-dockerclient"
)

//hide the docker client side types
type imageInspect struct {
	wrapped *docker.Image
}

//hide the docker client side types
type contInspect struct {
	wrapped *docker.Container
}

type Port string
type PortBinding struct {
	HostIp   string
	HostPort string
}

type RunConfig struct {
	Image      string
	Attach     bool
	Volumes    map[string]string
	Ports      map[Port][]PortBinding
	Links      map[string]string
	WaitOutput bool
}

type TagInfo struct {
	Repository string
	Tag        string
}

type BuildConfig struct {
	NoCache                  bool
	RemoveTemporaryContainer bool
}

type CopyArtifact struct {
	SourcePath, DestinationDir string
}

type DockerCli interface {
	CmdRun(*RunConfig, ...string) (*bytes.Buffer, string, error)
	CmdTag(string, bool, *TagInfo) error
	CmdCommit(string, *TagInfo) (string, error)
	CmdBuild(*BuildConfig, string, string) error
	//Copy actually does two different things: copies artifacts from the source tree into a tarball
	//or copies artifacts from a container (given here as an image) into a tarball.  In both cases
	//the resulting tarball is sent to the docker server for a build.
	CmdCopy(map[string]string, string, string, []*CopyArtifact, string) error
	CmdLastModTime(map[string]string, string, []*CopyArtifact) (time.Time, error)
	CmdStop(string) error
	InspectImage(string) (InspectedImage, error)
	InspectContainer(string) (InspectedContainer, error)
}

type InspectedImage interface {
	CreatedTime() time.Time
}

type InspectedContainer interface {
	Running() bool
	CreatedTime() time.Time
	ContainerName() string
	ExitStatus() int
}

//NewDocker returns a connection to the docker server.  Pickett assumes that
//the DockerCli is "passed in from the outside".
func NewDockerCli(debug, showDocker bool) (DockerCli, error) {
	if err := validateDockerHost(); err != nil {
		return nil, err
	}
	return newDockerCli(debug, showDocker)
}

type dockerCli struct {
	client     *docker.Client
	debug      bool
	showDocker bool
}

// newDockerCli builds a new docker interface and returns it. It
// assumes that the DOCKER_HOST env var has already been
// validated.
func newDockerCli(debug, showDocker bool) (DockerCli, error) {
	result := &dockerCli{
		debug:      debug,
		showDocker: showDocker,
	}
	var err error
	result.client, err = docker.NewClient(os.Getenv("DOCKER_HOST"))
	if err != nil {
		return nil, err
	}
	if showDocker {
		fmt.Printf("[docker cmd] export DOCKER_HOST='%s'\n", os.Getenv("DOCKER_HOST"))
	}
	return result, nil
}

func (d *dockerCli) createNamedContainer(config *docker.Config) (*docker.Container, error) {
	tries := 0
	ok := false
	var cont *docker.Container
	var err error
	var opts docker.CreateContainerOptions
	for tries < 3 {
		opts.Config = config
		opts.Name = newPhrase()
		cont, err = d.client.CreateContainer(opts)
		if err != nil {
			detail, ok := err.(*docker.Error)
			if ok && detail.Status == 409 {
				tries++
				continue
			} else {
				return nil, err
			}
		}
		ok = true
		break
	}
	if !ok {
		opts.Name = "" //fallback
		cont, err = d.client.CreateContainer(opts)
		if err != nil {
			return nil, err
		}
	}
	return cont, nil
}

var EMPTY struct{}

func (d *dockerCli) CmdRun(runconf *RunConfig, s ...string) (*bytes.Buffer, string, error) {
	config := &docker.Config{}
	config.Cmd = s
	config.Image = runconf.Image

	cont, err := d.createNamedContainer(config)
	if err != nil {
		return nil, "", err
	}
	host := &docker.HostConfig{}

	//flatten links for consumption by go-dockerclient
	flatLinks := []string{}
	for k, v := range runconf.Links {
		flatLinks = append(flatLinks, fmt.Sprintf("%s:%s", k, v))
	}
	host.Links = flatLinks
	host.Binds = []string{}
	for k, v := range runconf.Volumes {
		host.Binds = append(host.Binds, fmt.Sprintf("%s:%s", k, v))
	}

	//convert the types of the elements of this map so that *our* clients don't
	//see the inner types
	convertedMap := make(map[docker.Port][]docker.PortBinding)
	for k, v := range runconf.Ports {
		key := docker.Port(k)
		convertedMap[key] = []docker.PortBinding{}
		for _, m := range v {
			convertedMap[key] = append(convertedMap[key],
				docker.PortBinding{HostIp: m.HostIp, HostPort: m.HostPort})
		}
	}
	host.PortBindings = convertedMap

	err = d.client.StartContainer(cont.ID, host)
	if err != nil {
		return nil, "", err
	}

	if runconf.Attach {

		if runconf.WaitOutput {
			fmt.Fprintf(os.Stderr, "[pickett warning] shouldn't use WaitOutput with Attach, ignoring.\n")
		}

		//These are the right settings if you want to "watch" the output of the command and wait for
		//it to terminate
		err = d.client.AttachToContainer(docker.AttachToContainerOptions{
			Container:    cont.ID,
			OutputStream: os.Stdout,
			ErrorStream:  os.Stderr,
			Logs:         true,
			Stdout:       true,
			Stderr:       true,
			Stream:       true,
		})

		if err != nil {
			return nil, "", err
		}
		return nil, cont.ID, nil
	}

	//wait for result and return a buffer with the output
	if runconf.WaitOutput {
		_, err = d.client.WaitContainer(cont.ID)
		if err != nil {
			return nil, "", err
		}
		out := new(bytes.Buffer)
		err = d.client.AttachToContainer(docker.AttachToContainerOptions{
			Container:    cont.ID,
			OutputStream: out,
			ErrorStream:  out,
			Logs:         true,
			Stdout:       true,
			Stderr:       true,
		})
		if err != nil {
			return nil, "", err
		}

		return out, cont.ID, nil
	}

	//just start it and return with the id
	return nil, cont.ID, nil
}

func (d *dockerCli) CmdStop(contID string) error {
	fmt.Printf("TRYING TO STOP CONTAINER %s\n", contID)
	return d.client.StopContainer(contID, 10)
}

func (d *dockerCli) CmdTag(image string, force bool, info *TagInfo) error {

	return d.client.TagImage(image, docker.TagImageOptions{
		Force: force,
		Tag:   info.Tag,
		Repo:  info.Repository,
	})
}

func (d *dockerCli) CmdCommit(containerId string, info *TagInfo) (string, error) {
	opts := docker.CommitContainerOptions{
		Container: containerId,
	}
	if info != nil {
		opts.Tag = info.Tag
		opts.Repository = info.Repository
	}
	image, err := d.client.CommitContainer(opts)
	if err != nil {
		return "", err
	}
	return image.ID, nil
}

func (d *dockerCli) tarball(pathToDir string, localName string, tw *tar.Writer) error {
	if d.debug {
		fmt.Printf("[debug] tarball construction in '%s' (as '%s')\n", pathToDir, localName)
	}
	dir, err := os.Open(pathToDir)
	if err != nil {
		return err
	}
	info, err := dir.Stat()
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("expected %s to be a directory!", dir)
	}
	names, err := dir.Readdirnames(0)
	if err != nil {
		return err
	}
	for _, name := range names {
		path := filepath.Join(pathToDir, name)
		lname := filepath.Join(localName, name)
		isFile, err := d.writeFullFile(tw, path, lname)
		if err != nil {
			return err
		}
		if !isFile {
			err := d.tarball(path, filepath.Join(localName, name), tw)
			if err != nil {
				return err
			}
			continue
		}
	}
	return nil
}

func (d *dockerCli) writeFullFile(tw *tar.Writer, path string, localName string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	if info.IsDir() {
		return false, nil
	}

	//now we are sure it's a file
	hdr := &tar.Header{
		Name:    localName,
		Size:    info.Size(),
		Mode:    int64(info.Mode()),
		ModTime: info.ModTime(),
	}

	if err := tw.WriteHeader(hdr); err != nil {
		return false, err
	}
	fp, err := os.Open(path)
	if err != nil {
		return false, err
	}
	content, err := ioutil.ReadAll(fp)
	if err != nil {
		return false, err
	}
	if _, err := tw.Write(content); err != nil {
		return false, err
	}
	if d.debug {
		fmt.Printf("[debug] added %s as %s to tarball\n", path, localName)
	}
	return true, nil
}

//XXX is it safe to use /bin/true?
func (d *dockerCli) makeDummyContainerToGetAtImage(img string) (string, error) {
	cont, err := d.client.CreateContainer(docker.CreateContainerOptions{
		Config: &docker.Config{
			Image:      img,
			Entrypoint: []string{"/bin/true"},
		},
	})
	if err != nil {
		return "", err
	}
	return cont.ID, nil
}

func (d *dockerCli) CmdLastModTime(realPathSource map[string]string, img string,
	artifacts []*CopyArtifact) (time.Time, error) {
	if len(realPathSource) == len(artifacts) {
		if d.debug {
			fmt.Printf("[debug] no work to do in the container for last mod time, no artifacts inside it.\n")
			return time.Time{}, nil
		}
	}
	cont, err := d.makeDummyContainerToGetAtImage(img)
	if err != nil {
		return time.Time{}, err
	}
	err = d.client.StartContainer(cont, &docker.HostConfig{})
	if err != nil {
		return time.Time{}, err
	}
	//walk each artifact, getting it from the container, skipping sources
	best := time.Time{}
	for _, a := range artifacts {
		_, found := realPathSource[a.SourcePath]
		if found {
			continue
		}
		//pull it from container
		buf := new(bytes.Buffer)
		err = d.client.CopyFromContainer(docker.CopyFromContainerOptions{
			OutputStream: buf,
			Container:    cont,
			Resource:     a.SourcePath,
		})
		if err != nil {
			return time.Time{}, err
		}
		//var out bytes.Buffer
		r := bytes.NewReader(buf.Bytes())
		tr := tar.NewReader(r)
		for {
			entry, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return time.Time{}, err
			}
			if d.debug {
				fmt.Printf("[debug] read file from container: %s, %v\n", entry.Name, entry.ModTime)
			}
			if !entry.FileInfo().IsDir() {
				if entry.ModTime.After(best) {
					best = entry.ModTime
				}
			}
		}
	}
	return best, nil
}

func (d *dockerCli) CmdCopy(realPathSource map[string]string, imgSrc string, imgDest string,
	artifacts []*CopyArtifact, resultTag string) error {
	cont, err := d.makeDummyContainerToGetAtImage(imgSrc)
	if err != nil {
		return err
	}

	if len(realPathSource) != len(artifacts) {
		if d.debug {
			fmt.Printf("[debug] starting container because we need to retrieve artifacts from it\n")
		}
		//don't bother starting the container untless there is something we need from it
		err = d.client.StartContainer(cont, &docker.HostConfig{})
		if err != nil {
			return err
		}
	} else {
		if d.debug {
			fmt.Printf("[debug] all artifacts found in source tree, not starting container\n")
		}
	}

	dockerFile := new(bytes.Buffer)
	resulTarball := new(bytes.Buffer)
	tw := tar.NewWriter(resulTarball)

	dockerFile.WriteString(fmt.Sprintf("FROM %s\n", imgDest))

	//walk each artifact, potentially getting it from the container
	for _, a := range artifacts {
		truePath, found := realPathSource[a.SourcePath]
		if found {
			isFile, err := d.writeFullFile(tw, truePath, a.SourcePath)
			if err != nil {
				return err
			}
			//kinda hacky: we use a.SourcePath as the name *inside* the tarball so we can get the
			//directory name right on the final output
			dockerFile.WriteString(fmt.Sprintf("COPY %s %s\n", a.SourcePath, a.DestinationDir))
			if !isFile {
				if err := d.tarball(truePath, a.SourcePath, tw); err != nil {
					return err
				}
			}
		} else {
			//pull it from container
			buf := new(bytes.Buffer)
			err = d.client.CopyFromContainer(docker.CopyFromContainerOptions{
				OutputStream: buf,
				Container:    cont,
				Resource:     a.SourcePath,
			})
			if err != nil {
				return err
			}
			//var out bytes.Buffer
			r := bytes.NewReader(buf.Bytes())
			tr := tar.NewReader(r)
			for {
				entry, err := tr.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					return err
				}
				if d.debug {
					fmt.Printf("[debug] read file from container: %s\n", entry.Name)
				}
				if !entry.FileInfo().IsDir() {
					dockerFile.WriteString(fmt.Sprintf("COPY %s %s\n", entry.Name, a.DestinationDir+"/"+entry.Name))
					if err := tw.WriteHeader(entry); err != nil {
						return err
					}
					if _, err := io.Copy(tw, tr); err != nil {
						return err
					}
				}
			}
		}
	}

	hdr := &tar.Header{
		Name: "Dockerfile",
		Size: int64(dockerFile.Len()),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	if _, err := io.Copy(tw, dockerFile); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}

	opts := docker.BuildImageOptions{
		Name:           resultTag,
		InputStream:    resulTarball,
		OutputStream:   os.Stdout,
		RmTmpContainer: true,
		SuppressOutput: false,
		NoCache:        true,
	}

	if err := d.client.BuildImage(opts); err != nil {
		return err
	}

	return nil
}

func (d *dockerCli) CmdBuild(config *BuildConfig, pathToDir string, tag string) error {

	//build tarball
	out := new(bytes.Buffer)
	tw := tar.NewWriter(out)
	err := d.tarball(pathToDir, "", tw)
	if err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	opts := docker.BuildImageOptions{
		Name:           tag,
		InputStream:    bytes.NewBuffer(out.Bytes()),
		OutputStream:   os.Stdout,
		RmTmpContainer: config.RemoveTemporaryContainer,
		SuppressOutput: false,
		NoCache:        config.NoCache,
	}

	if err := d.client.BuildImage(opts); err != nil {
		return err
	}
	return nil
}

func (c *dockerCli) InspectImage(n string) (InspectedImage, error) {
	i, err := c.client.InspectImage(n)
	if err != nil {
		return nil, err
	}
	return &imageInspect{
		wrapped: i,
	}, nil
}

func (c *dockerCli) InspectContainer(n string) (InspectedContainer, error) {
	i, err := c.client.InspectContainer(n)
	if err != nil {
		return nil, err
	}
	return &contInspect{
		wrapped: i,
	}, nil
}

//Wrappers for getting inspections
func (i *imageInspect) CreatedTime() time.Time {
	return i.wrapped.Created
}

func (c *contInspect) CreatedTime() time.Time {
	return c.wrapped.Created
}

func (c *contInspect) Running() bool {
	return c.wrapped.State.Running
}

func (c *contInspect) ContainerName() string {
	return c.wrapped.Name
}

func (c *contInspect) ExitStatus() int {
	return c.wrapped.State.ExitCode
}
