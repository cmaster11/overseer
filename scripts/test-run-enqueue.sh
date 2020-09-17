#!/usr/bin/env bash
set -Eeuxmo pipefail
DIR="$(dirname "$(command -v greadlink >/dev/null 2>&1 && greadlink -f "$0" || readlink -f "$0")")"

TMP_FILE=$(mktemp)
cat >"$TMP_FILE" <<EOL
fake-name1 must run dumb-test with retries 0 with dumb-duration-min 1s with dumb-duration-max 5s
fake-name2 must run dumb-test with retries 0 with dumb-duration-min 1s with dumb-duration-max 5s
fake-name3 must run dumb-test with retries 0 with dumb-duration-min 1s with dumb-duration-max 5s
fake-name4 must run dumb-test with retries 0 with dumb-duration-min 1s with dumb-duration-max 5s
EOL

go run "$DIR/.." enqueue "$TMP_FILE"