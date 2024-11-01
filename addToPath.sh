cd "$(dirname "$0")"

addToPathFront() {
  if [ -d "$1" ]; then
    if [[ "$PATH" != *"$1"* ]]; then
      export PATH=$1:$PATH
    fi
  fi
}

HOST=$(go env | grep GOHOSTOS | cut -d'=' -f2)
ARCH=$(go env | grep GOARCH | cut -d'=' -f2)

addToPathFront ./lib/$HOST/$ARCH/bin