#!/usr/bin/env bash
set -Eeuxmo pipefail
DIR="$(dirname "$(command -v greadlink >/dev/null 2>&1 && greadlink -f "$0" || readlink -f "$0")")"

# Use only with non-parallel worker

TMP_FILE=$(mktemp)
cat >"$TMP_FILE" <<EOL
dumb-test1 must run dumb-test with dumb-duration-min 2s with dumb-duration-max 2s with min-duration 3s with retries 0 with fail-at 0 with test-label "No alert"
dumb-test1 must run dumb-test with dumb-duration-min 2s with dumb-duration-max 2s with min-duration 3s with retries 0 with fail-at 0 with test-label "No alert"
dumb-test1 must run dumb-test with dumb-duration-min 2s with dumb-duration-max 2s with min-duration 3s with retries 0 with fail-at 0 with test-label "Should alert (3s have passed)"
dumb-test1 must run dumb-test with dumb-duration-min 2s with dumb-duration-max 2s with min-duration 3s with retries 0 with fail-at 98 with test-label "Should recover"
dumb-test1 must run dumb-test with dumb-duration-min 2s with dumb-duration-max 2s with min-duration 3s with retries 0 with fail-at 0 with test-label "No alert"
dumb-test1 must run dumb-test with dumb-duration-min 5s with dumb-duration-max 5s with min-duration 3s with retries 0 with fail-at 99 with test-label "No recover message (alert not shown)"
dumb-test1 must run dumb-test with dumb-duration-min 2s with dumb-duration-max 2s with min-duration 3s with retries 0 with fail-at 99 with test-label "No recover message"
dumb-test1 must run dumb-test with dumb-duration-min 2s with dumb-duration-max 2s with min-duration 3s with retries 0 with fail-at 99 with test-label "No recover message"
dumb-test1 must run dumb-test with dumb-duration-min 2s with dumb-duration-max 2s with min-duration 3s with retries 0 with fail-at 99 with test-label "No recover message"
EOL

go run "$DIR/.." enqueue "$TMP_FILE"