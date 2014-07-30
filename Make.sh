#!/bin/sh -x

if [ "$GOPATH" = "" ]; then
	echo "GOPATH not set? Trying to set GOPATH=$HOME/go (this may not work)"
	# Set up GOPATH in your home directory:
	export GOPATH=$HOME/go
	mkdir -p $GOPATH/src
	export PATH=$PATH:$GOPATH/bin
fi

# Get and run godep over pickett sources
go get github.com/tools/godep
godep get github.com/igneous-systems/pickett/pickett
# Install pickett
go install github.com/igneous-systems/pickett/pickett
