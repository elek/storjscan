#!/usr/bin/env bash
rm executors
for i in `seq 1 5`; do echo test$i >> executors; done
cat executors | xargs -INAME docker-compose -p NAME up -d

