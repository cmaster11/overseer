#!/usr/bin/env bash
set -Eeuxmo pipefail
DIR="$(dirname "$(command -v greadlink >/dev/null 2>&1 && greadlink -f "$0" || readlink -f "$0")")"

TMP_FILE=$(mktemp)
cat >"$TMP_FILE" <<EOL
dumb-test1 must run dumb-test with dumb-duration-max 100ms with pt-duration 5s with pt-sleep 200ms with pt-threshold 0%
dumb-test2 must run dumb-test with dumb-duration-max 100ms with pt-duration 5s with pt-sleep 200ms with pt-threshold 20%
dumb-test3 must run dumb-test with dumb-duration-max 100ms with pt-duration 5s with pt-sleep 200ms with pt-threshold 40%
dumb-test4 must run dumb-test with dumb-duration-max 100ms with pt-duration 5s with pt-sleep 200ms with pt-threshold 60%
EOL

go run "$DIR/.." enqueue "$TMP_FILE"