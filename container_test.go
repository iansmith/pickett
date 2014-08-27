package pickett

import (
	"strings"
	"testing"
	"time"

	"code.google.com/p/gomock/gomock"

	"github.com/igneous-systems/pickett/io"
)

const (
	DIR    = "/foo/bar/baz/mydir" //as if the file content lives in this dir
	SOMEID = "abcdef012345678"
	BLETCH = "blah:bletch"
	MYDIR  = "mydir"
)

func TestAfterBuildTimeIsUpdated(t *testing.T) {

	controller := gomock.NewController(t)
	defer controller.Finish()

	cli := io.NewMockDockerCli(controller)
	helper := io.NewMockHelper(controller)
	etcd := io.NewMockEtcdClient(controller)

	//ignore debug messages
	helper.EXPECT().Debug(gomock.Any(), gomock.Any()).AnyTimes()
	helper.EXPECT().Debug(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	//for reading the conf
	helper.EXPECT().CheckFatal(gomock.Nil(), gomock.Any()).AnyTimes()
	helper.EXPECT().OpenDockerfileRelative(MYDIR).Return(nil, nil)

	//fake out the directory "true" path
	helper.EXPECT().DirectoryRelative(MYDIR).Return(DIR)

	//ignoring error is ok because tested in TestConf
	c, _ := NewConfig(strings.NewReader(example1), helper, cli, etcd)

	now := time.Now()
	thirtyAgo := now.Add(-30 * time.Minute)
	hourAgo := now.Add(-1 * time.Hour)

	//directory is files, modified 30mins ago
	helper.EXPECT().LastTimeInDirRelative(MYDIR).Return(thirtyAgo, nil)

	//two fake Inspecteds of the tag "blah/bletch"
	hourStamp := io.NewMockInspectedImage(controller)
	hourStamp.EXPECT().CreatedTime().Return(hourAgo)

	nowStamp := io.NewMockInspectedImage(controller)
	nowStamp.EXPECT().CreatedTime().Return(now)

	//hook inspecteds to calls to Inspect in ORDER
	first := cli.EXPECT().InspectImage(BLETCH).Return(hourStamp, nil)
	cli.EXPECT().InspectImage(BLETCH).Return(nowStamp, nil).After(first)

	//get this after the first time check comparing directry time to hourStamp
	cli.EXPECT().CmdBuild(gomock.Any(), DIR, BLETCH).Return(nil)

	///
	//at start, we don't know antyhing about the time
	//
	node := c.nameToNode["blah:bletch"]
	if !node.time().IsZero() {
		t.Fatalf("failed to initialize times correctly: %v\n", node.time())
	}
	c.Build("blah:bletch")

	//
	// we have rebuilt, check the time on the node
	//
	if node.time() != now {
		t.Fatalf("failed to update the time correctly: %v\n", node.time())
	}

}
