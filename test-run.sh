#!/usr/bin/env bash
export STORJ_EXECUTORS=`pwd`/executors
go test -v -parallel 2 -p 20 -json -exec `pwd`/test.sh ./... | tee tests.json

