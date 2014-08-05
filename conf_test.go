package pickett

import (
	"strings"
	"testing"

	"code.google.com/p/gomock/gomock"

	"github.com/igneous-systems/pickett/io"
)

var example1 = `
// example1
{
	"DockerBuildOptions" : {
	},

	// a comment
	"CodeVolume" : [
		{
			"Directory" : "src", //will expand to /home/gredo/src
			"MountedAt" : "/han",  // stray comma?,
			"SomeExtra" : "cruft"
		}
	],
	"Containers" : [
		{
			"Tag" : "blah/bletch",
			"Directory" : "mydir"
		}
	],
	"GoBuilds" : [
		{
			"Repository": "test",
			"RunIn" : "blah/bletch",
			"Packages": ["p1...", "p2/p3" ],
			"Command" : "go test",
			"Tag": "nashville"
		},
		{
			"Repository": "fart",
			"RunIn" : "blah/bletch",
			"Packages": ["p4...", "p5/p6" ],
			"Tag": "chattanooga"
		}
	]
}
`

func setupForExample1Conf(controller *gomock.Controller, helper *io.MockHelper) {
	//ignore debug messages
	helper.EXPECT().Debug(gomock.Any(), gomock.Any()).AnyTimes()
	helper.EXPECT().Debug(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	//for reading the conf
	helper.EXPECT().CheckFatal(gomock.Nil(), gomock.Any()).AnyTimes()
	helper.EXPECT().OpenDockerfileRelative("mydir").Return(nil, nil)
}

func TestConf(t *testing.T) {
	controller := gomock.NewController(t)
	defer controller.Finish()

	helper := io.NewMockHelper(controller)
	cli := io.NewMockDockerCli(controller)
	helper.EXPECT().CheckFatal(gomock.Nil(), gomock.Any()).AnyTimes()

	//the caller is just opening this for the error return, he ignores the file
	helper.EXPECT().OpenDockerfileRelative("mydir").Return(nil, nil)

	c, err := NewConfig(strings.NewReader(example1), helper, cli, nil, nil)
	if err != nil {
		t.Fatalf("can't parse legal config file: %v", err)
	}
	if c.CodeVolumes[0].Directory != "src" {
		t.Errorf("failed to parse CodeVolume>Directory")
	}
}
