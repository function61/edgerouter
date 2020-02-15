#!/bin/bash -eu

if [ ! -L "/usr/local/bin/er" ]; then
	ln -s /workspace/rel/edgerouter_linux-amd64 /usr/local/bin/er
fi

source /build-common.sh

BINARY_NAME="edgerouter"
COMPILE_IN_DIRECTORY="cmd/edgerouter"

standardBuildProcess
