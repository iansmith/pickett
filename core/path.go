package pickett

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// needsPathTranslation determines whether we should map from $HOME to /vagrant.
// Right now, we do so if the transport specified by DOCKER_HOST is TCP.
func needsPathTranslation() bool {
	if strings.HasPrefix(os.Getenv("DOCKER_HOST"), "tcp://") {
		return true
	}
	flog.Debugf("DOCKER_HOST isn't tcp://; assuming it's local and skipping path translation")
	return false
}

// translatePath takes a path rooted at $HOME and re-roots it at /vagrant.
func translatePath(path string) (string, error) {
	if os.Getenv("HOME") == "" {
		return "", errors.New("You dont have a HOME environment variable set, can't even guess a vagrant mapping!")
	}
	home := strings.TrimRight(os.Getenv("HOME"), "/") + "/"
	if !strings.HasPrefix(path, home) {
		return "", errors.New(fmt.Sprintf("Cant guess a /vagrant mapping from %s", path))
	}
	result := "/vagrant/" + path[len(home):]
	flog.Debugf("no virtualbox mappings, guessing %s -> %s...", path, result)
	return result, nil
}
