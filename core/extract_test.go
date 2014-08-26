package pickett

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/igneous-systems/pickett/io"

	"code.google.com/p/gomock/gomock"
)

var extractExample = `
{
	"DockerBuildOptions" : {
	},

	// a comment
	"CodeVolumes" : [
		{
			"Directory" : "src", //will expand to /home/gredo/src
			"MountedAt" : "/han"
		}
	],
	"Extractions" : [
		{
			"Repository": "extractTest",
			"Tag": "test1",
			"RunIn" : "someimage",
			"MergeWith": "someotherimage",
			"Artifacts": [
				{
					"BuiltPath":"/opt/somebuild/product",
					"DestinationDir":"/place/to/put/it"
				}
			]
		},
		{
			"Repository": "extractTest",
			"Tag": "test2",
			"RunIn" : "someimage",
			"MergeWith": "someotherimage",
			"Artifacts": [
				{
					"BuiltPath":"/han/foobie.tgz",
					"DestinationDir":"/dest/foobie.tgz"
				}
			]
		}

	]
}
`

func TestExtractFromSource(t *testing.T) {
	controller := gomock.NewController(t)
	defer controller.Finish()

	SOMEIMAGE := "someimage"
	SOMEOTHERIMAGE := "someotherimage"
	EXTRACT2 := "extractTest:test2"
	SRC := "src"
	TRUESRC := "/home/gredo/src"
	HAN := "/han"
	FOOBIE := "foobie.tgz"

	docker := io.NewMockDockerCli(controller)
	helper := io.NewMockHelper(controller)
	etcd := io.NewMockEtcdClient(controller)

	//will check to see if the images are present in config parsing
	//we act as though it is present
	insp := io.NewMockInspectedImage(controller)
	docker.EXPECT().InspectImage(SOMEIMAGE).Return(insp, nil).Times(2)
	docker.EXPECT().InspectImage(SOMEOTHERIMAGE).Return(insp, nil).Times(2)

	conf, err := NewConfig(strings.NewReader(extractExample), helper, docker, etcd)
	if err != nil {
		t.Fatalf("can't parse legal config file: %v", err)
	}

	//will check to see if the images are present in config parsing
	//we act as though it is present
	docker.EXPECT().InspectImage(EXTRACT2).Return(nil, fmt.Errorf("will not be looked at"))
	helper.EXPECT().DirectoryRelative(SRC).Return(TRUESRC)

	//last time on dir
	now := time.Now()
	hourAgo := now.Add(-1 * time.Hour)
	helper.EXPECT().LastTimeInDir(TRUESRC+"/"+FOOBIE).Return(hourAgo, nil)

	//making tarball
	helper.EXPECT().CopyFileToTarball(gomock.Any(), TRUESRC+"/"+FOOBIE, HAN+"/"+FOOBIE).Return(true, nil)

	//build from the tarball
	docker.EXPECT().CmdBuildFromTarball(gomock.Any(), gomock.Any(), EXTRACT2).Return(nil)

	after := io.NewMockInspectedImage(controller)
	after.EXPECT().CreatedTime().Return(now).Times(2)
	docker.EXPECT().InspectImage(EXTRACT2).Return(after, nil)

	//kick off the extract procedure, this time on test2
	if err := conf.Build("extractTest:test2"); err != nil {
		t.Fatalf("unexpected error in Build(): %v", err)
	}
}

func TestExtractBuild(t *testing.T) {
	controller := gomock.NewController(t)
	defer controller.Finish()

	SOMEIMAGE := "someimage"
	SOMEOTHERIMAGE := "someotherimage"
	EXTRACT1 := "extractTest:test1"
	SRC := "src"
	TRUESRC := "/home/gredo/src"

	docker := io.NewMockDockerCli(controller)
	helper := io.NewMockHelper(controller)
	etcd := io.NewMockEtcdClient(controller)

	//will check to see if the images are present in config parsing
	//we act as though it is present
	insp := io.NewMockInspectedImage(controller)
	docker.EXPECT().InspectImage(SOMEIMAGE).Return(insp, nil).Times(2)
	docker.EXPECT().InspectImage(SOMEOTHERIMAGE).Return(insp, nil).Times(2)

	conf, err := NewConfig(strings.NewReader(extractExample), helper, docker, etcd)
	if err != nil {
		t.Fatalf("can't parse legal config file: %v", err)
	}

	//first we fake that the tag does not exist
	docker.EXPECT().InspectImage(EXTRACT1).Return(nil, fmt.Errorf("will not be examined"))
	//tell him about the source mapping
	helper.EXPECT().DirectoryRelative(SRC).Return(TRUESRC)

	//pull the content from the container
	docker.EXPECT().CmdRetrieve(gomock.Any(), gomock.Any(), []*io.CopyArtifact{
		&io.CopyArtifact{
			SourcePath:     "/opt/somebuild/product",
			DestinationDir: "/place/to/put/it",
		},
	}, SOMEIMAGE).Return(nil)

	//watch for the build from the resulting tarball
	docker.EXPECT().CmdBuildFromTarball(gomock.Any(), gomock.Any(), EXTRACT1).Return(nil)

	//interrogation to see what the time is
	ext := io.NewMockInspectedImage(controller)
	now := time.Now()
	ext.EXPECT().CreatedTime().Return(now).Times(2)
	docker.EXPECT().InspectImage(EXTRACT1).Return(ext, nil)

	//kick off the extract procedure
	if err := conf.Build("extractTest:test1"); err != nil {
		t.Fatalf("unexpected error in Build(): %v", err)
	}

}
