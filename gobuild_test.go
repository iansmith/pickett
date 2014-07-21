package pickett

import (
	"errors"
	"strings"
	"testing"
	"time"

	"code.google.com/p/gomock/gomock"
	docker_utils "github.com/dotcloud/docker/utils"

	"github.com/igneoussystems/pickett/io"
)

func setupForDontBuildBletch(controller *gomock.Controller, helper *io.MockIOHelper, cli *io.MockDockerCli, tag string) *Config {
	setupForExample1Conf(controller, helper)
	//ignoring error is ok because tested in TestConf
	c, _ := NewConfig(strings.NewReader(example1), helper)

	//fake out the building of bletch
	now := time.Now()
	hourAgo := now.Add(-1 * time.Hour)
	helper.EXPECT().LastTimeInDirRelative("mydir").Return(hourAgo, nil)
	insp := io.NewMockInspected(controller)
	insp.EXPECT().CreatedTime().Return(now)
	cli.EXPECT().DecodeInspect("blah/bletch").Return(insp, nil)

	fakeInspectError := &docker_utils.StatusError{
		StatusCode: 1,
	}

	helper.EXPECT().DirectoryRelative("src").Return("/home/gredo/src")
	cli.EXPECT().DecodeInspect(tag).Return(nil, fakeInspectError)

	return c
}

func TestGoPackagesFailOnBuildStep2(t *testing.T) {
	controller := gomock.NewController(t)
	defer controller.Finish()

	cli := io.NewMockDockerCli(controller)
	helper := io.NewMockIOHelper(controller)

	c := setupForDontBuildBletch(controller, helper, cli, "chattanooga")

	// mock out the docker api calls to build the software
	first := cli.EXPECT().CmdRun("-v", "/home/gredo/src:/han", "blah/bletch", "go", "install",
		"p4...")
	fakeErr := errors.New("whoa doggie")
	cli.EXPECT().CmdRun("-v", "/home/gredo/src:/han", "blah/bletch", "go", "install",
		"p5/p6").Return(fakeErr).After(first)

	if err := c.Initiate("chattanooga", helper, cli); err != fakeErr {
		t.Errorf("failed to get expected error: %v", err)
	}
}

func TestGoPackagesAllBuilt(t *testing.T) {
	controller := gomock.NewController(t)
	defer controller.Finish()

	cli := io.NewMockDockerCli(controller)
	helper := io.NewMockIOHelper(controller)

	c := setupForDontBuildBletch(controller, helper, cli, "nashville")

	// mock out the docker api calls to build the software
	first := cli.EXPECT().CmdRun("-v", "/home/gredo/src:/han", "blah/bletch", "go", "test",
		"p1...")
	cli.EXPECT().CmdRun("-v", "/home/gredo/src:/han", "blah/bletch", "go", "test",
		"p2/p3").After(first)
	cli.EXPECT().CmdPs("-q", "-l").Return(nil)
	fakeContainer := "1234ffee5678"
	cli.EXPECT().LastLineOfStdout().Return(fakeContainer)
	cli.EXPECT().CmdCommit(fakeContainer, "nashville")

	c.Initiate("nashville", helper, cli)

}
