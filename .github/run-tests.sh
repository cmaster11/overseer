#!/bin/sh
set -e
DIR="$(dirname "$(command -v greadlink >/dev/null 2>&1 && greadlink -f "$0" || readlink -f "$0")")"

# Install tools to test our code-quality.
go install "$DIR/shadow-fix.go"
go get -u honnef.co/go/tools/cmd/staticcheck

# Run the static-check tool
t=$(mktemp)
staticcheck ./... >$t || true
if [ -s $t ]; then
  echo "Found errors via 'staticcheck'"
  cat $t
  rm $t
  exit 1
fi
rm $t

# Run the shadow-checker
echo "Launching shadowed-variable check .."
go vet -vettool=$(which shadow-fix) ./...
echo "Completed shadowed-variable check .."

# Run golang tests
go test -v ./...
