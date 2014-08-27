#!/bin/bash

set -xe

if [[ -n "$(git status --porcelain --untracked-files=no)" ]]; then
	echo "Unclean git repo: bailing on release"
	exit 1
fi

# Capture git rev for the binary versioning
COMMIT=$(git rev-parse HEAD)

# Force the building of linux_amd64 and darwin_amd64 binaries
for os in linux ; do
	export GOOS=$os
	export GOARCH=amd64

	OUTPUT=pickett-$GOOS-$GOARCH-$COMMIT
	RELURL=http://igneous-dev.s3.amazonaws.com/pickett-releases/$OUTPUT.bin

	go build -o $OUTPUT ./pickett

	if [[ "$1" = "-u" ]]; then
		./s3curl.pl \
			--id ig \
			--acl public-read \
			--put $OUTPUT \
			-- -o /dev/null "$RELURL"
		echo Release uploaded to $RELURL
	fi
	rm -f $OUTPUT

done

echo SHA=$COMMIT
