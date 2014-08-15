package io

import (
	"io"
	"os"
	"path/filepath"
	"time"
)

// Helper abstracts IO to make things easier to test.  It is responsible for things
// that touch the filesystem, fatally exit, or stdout/stderr.
type Helper interface {
	OpenDockerfileRelative(dir string) (io.Reader, error)
	OpenFileRelative(path string) (*os.File, error)
	DirectoryRelative(dir string) string
	ConfigReader() io.Reader
	ConfigFile() string
	LastTimeInDirRelative(string) (time.Time, error)
	LastTimeInDir(string) (time.Time, error)
}

// NewHelper creates an implementation of the Helper that runs against
// a real filesystem.  This is used when running normally.  The parameter
// filepath should be a path to a Pickett.json file.  This should be a
// fully qualified path (starting with /).
func NewHelper(fullPath string) (Helper, error) {
	_, err := os.Open(fullPath)
	if err != nil {
		return nil, err
	}
	return &helper{
		pickettDir: filepath.Dir(fullPath),
		confFile:   fullPath,
	}, nil
}

// helper is the normal impl of Helper.
type helper struct {
	pickettDir string
	confFile   string
}

// OpenDockerfileRelative returns a reader connected to the Dockerfile
// requsted in dir (relative to the Pickett.json) or an error.
func (i *helper) OpenDockerfileRelative(dir string) (io.Reader, error) {
	return os.Open(filepath.Join(i.DirectoryRelative(dir), "Dockerfile"))
}

// OpenFiles returns an *os.File connected to file path given, relative to the
// configuration file.
func (i *helper) OpenFileRelative(path string) (*os.File, error) {
	dir := filepath.Dir(path)
	return os.Open(filepath.Join(i.DirectoryRelative(dir), filepath.Base(path)))
}

// Return the true directory of a given directory that is relative to the
// pickett config file.
func (i *helper) DirectoryRelative(dir string) string {
	return filepath.Clean(filepath.Join(i.pickettDir, dir))
}

// Return a reader hooked to the configuration file we were initial
func (i *helper) ConfigReader() io.Reader {
	rd, err := os.Open(i.confFile)
	//we checked this on creation, so should not fail
	if err != nil {
		flog.Criticalf("configuration file changed out from under us: %s : %v", i.confFile, err)
	}
	return rd
}

// ConfigFile returns the name of the original configuration file
// used to construct this object.
func (i *helper) ConfigFile() string {
	return i.confFile
}

func (i *helper) LastTimeInDirRelative(relative string) (time.Time, error) {
	dir := i.DirectoryRelative(relative)
	return lastTimeInADirTree(dir, time.Time{})
}

func (i *helper) LastTimeInDir(fullPath string) (time.Time, error) {
	return lastTimeInADirTree(fullPath, time.Time{})
}

//lastTimeInADirTree recursively traverses a directory and looks for
//the latest time it can find.
func lastTimeInADirTree(path string, bestSoFar time.Time) (time.Time, error) {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}, err
	}
	if !info.IsDir() {
		if info.ModTime().After(bestSoFar) {
			return info.ModTime(), nil
		}
		return bestSoFar, nil
	}
	fp, err := os.Open(path)
	if err != nil {
		return time.Time{}, err
	}
	names, err := fp.Readdirnames(0)
	if err != nil {
		return time.Time{}, err
	}
	best := bestSoFar
	for _, name := range names {
		child := filepath.Join(path, name)
		t, err := lastTimeInADirTree(child, best)
		if err != nil {
			return time.Time{}, err
		}
		if t.After(best) {
			best = t
		}
	}
	return best, nil
}
