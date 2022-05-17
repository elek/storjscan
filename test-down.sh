#!/usr/bin/env bash
rm executors
for i in `seq 1 5`; do echo test$i >> executors; done
cat executors | xargs -INAME docker-compose -p NAME up -d
export STORJ_EXECUTORS=`pwd`/executors
go test -v -parallel 2 -p 20 -exec `pwd`/test.sh ./...
cat executors | xargs -INAME docker-compose -p NAME down -v

