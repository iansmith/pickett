package pickett

import (
	"strings"
	"testing"

	"code.google.com/p/gomock/gomock"

	"github.com/igneoussystems/pickett/mock_pickett"
)

var example1 = `
// example1
{
	"DockerBuildOptions" : ["-foo", "-bar"],
	// a comment
	"CodeVolume" : {
		"Directory" : "/home/greedo/src",
		"MountedAt" : "/mesa",  // stray comma?,
		"SomeExtra" : "cruft"
	},
	"Sources" : [
		{
			"Tag" : "blah/bletch",
			"Directory" : "mydir"
		}
	],
	"Builds" : [
		{
			"RunIn" : "blah/bletch",
			"InstallAndTestGoPackages": ["p1..", "p2/p3" ],
			"Tag": "nashville"
		},
		{
			"RunIn" : "blah/bletch",
			"InstallAndTestGoPackages": ["p1..", "p2/p3" ],
			"Tag": "chattanooga"
		}
	]
}
`

func TestGoPackages(t *testing.T) {
	c, err := NewConfigFile(strings.NewReader(example1))
	if err != nil {
		t.Errorf("Can't process correct config: %v", err)
	}
	if len(c.buildNames) == 2 {
		if c.buildNames[0] != "nashville" || c.buildNames[1] != "chattanooga" {
			t.Errorf("bad build names %s %s", c.buildNames[0], c.buildNames[1])
		}
	} else {
		t.Errorf("wrong number of build names: %d", len(c.buildNames))
	}
}

func TestConf(t *testing.T) {
	controller := gomock.NewController(t)
	helper := NewMockIOHelper(controller)
	helper.EXPECT().Find

	c, err := NewConfig(strings.NewReader(example1))
	if err != nil {
		t.Fatalf("can't parse legal config file: %v", err)
	}
	if c.CodeVolume.Directory != "/home/greedo/src" {
		t.Errorf("failed to parse CodeVolume>Directory")
	}
	if len(c.DockerBuildOptions) != 2 {
		t.Errorf("failed to parse DockerBuildOptions")
	} else {
		if c.DockerBuildOptions[0] != "-foo" || c.DockerBuildOptions[1] != "-bar" {
			t.Errorf("failed to parse DockerBuildOptions: %v", c.DockerBuildOptions)
		}
	}
}
