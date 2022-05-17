#!/usr/bin/env bash
set -x
CONTAINER=$(shuf $STORJ_EXECUTORS | head -n1)-test-1
docker exec $CONTAINER mkdir -p $(dirname $1)
docker cp $1 $CONTAINER:$1
docker exec $CONTAINER "$@"

