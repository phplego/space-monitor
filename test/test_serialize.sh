#!/usr/bin/env bash

SELF=$(basename ${BASH_SOURCE[0]})
GO_FILE=${SELF/.sh/.go}
go run $GO_FILE
