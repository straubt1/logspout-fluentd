#!/bin/sh
set -e
mkdir -p /go/src/github.com/gliderlabs
cp -r /src /go/src/github.com/gliderlabs/logspout

# backwards compatibility
ln -fs /tmp/docker.sock /var/run/docker.sock
