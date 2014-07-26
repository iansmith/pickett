package io

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

var (
	NO_DOCKER_HOST         = errors.New("no DOCKER_HOST found in environment, please set it")
	BAD_DOCKER_HOST_FORMAT = errors.New("DOCKER_HOST found, but should be protocol://host:port")
	BAD_INSPECT_RESULT     = errors.New("unable to understand result of docker inspect")
)

const (
	PICKETT_KEYSPACE = "/pickett/"
)

//splitProto returns two strings based on an expected value of DOCKER_HOST
//that is foo://bar:baz.  It presumes you have already called validateDockerHost.
func splitProto() []string {
	if strings.Index(os.Getenv("DOCKER_HOST"), "://") == -1 {
		return nil
	}
	return strings.Split(os.Getenv("DOCKER_HOST"), "://")
}

//validateDockerHost checks the environment for a sensible value for DOCKER_HOST.
func validateDockerHost() error {
	raw := os.Getenv("DOCKER_HOST")
	if raw == "" {
		return NO_DOCKER_HOST
	}
	if strings.Index(raw, "://") == -1 {
		return BAD_DOCKER_HOST_FORMAT
	}
	parts := strings.Split(raw, "://")
	second := parts[1]
	if len(strings.Split(second, ":")) != 2 {
		if parts[0] == "unix" {
			return nil
		}
		return BAD_DOCKER_HOST_FORMAT
	}
	return nil
}

//construct ectd host from splitProto, assumes validateDockerHost already called
func constructEctdHost() string {
	pair := splitProto()
	if pair[0] == "unix" {
		return "http://localhost:4001"
	}
	hostPort := strings.Split(pair[1], ":")
	if len(hostPort) != 2 {
		fmt.Fprintf(os.Stderr, "your DOCKER_HOST is probably bogus (%s) and should be 'tcp://host:port'\n",
			os.Getenv("DOCKER_HOST"))
	}
	return "http://" + hostPort[0] + ":4001"
}
