#!/bin/bash
mock_suffix="_mock.go"
files=( $(find . -name "*$mock_suffix") )
for f in "${files[@]}"
do
    source="${f/$mock_suffix/.go}"
    package=$(basename $(dirname $f))
    echo "$source -> $f (package $package)"
    mockgen -source=$source -package=$package > $f
done
