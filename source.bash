#!/bin/bash
SCRIPT_DIR=$(dirname "$(realpath "$BASH_SOURCE")")

HOST=$(go env | grep GOHOSTOS | cut -d'=' -f2 | tr -d "'")
ARCH=$(go env | grep GOARCH | cut -d'=' -f2 | tr -d "'")

export PATH=$SCRIPT_DIR/lib/$HOST/$ARCH/bin:$PATH
