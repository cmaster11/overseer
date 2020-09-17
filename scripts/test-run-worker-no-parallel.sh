#!/usr/bin/env bash
set -Eeuxmo pipefail
DIR="$(dirname "$(command -v greadlink >/dev/null 2>&1 && greadlink -f "$0" || readlink -f "$0")")"

# Run worker with ipv6 disabled (local testing)
go run "$DIR/.." worker -6=false -verbose -tag Testtt -parallel 1
