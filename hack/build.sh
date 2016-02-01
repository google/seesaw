#!/bin/bash

OLD_PWD=`pwd`
SCRIPT=$(readlink -f "${BASH_SOURCE[0]}")
cd $(dirname $SCRIPT)
mkdir -p ../_out
rm -f ../_out/*
docker build -t 'seewas-dev:master'  ../
echo $HACK_DIR
docker run -i -t --rm=true -v $(readlink -f ../_out):/go/bin/out seewas-dev:master
cd $OLD_PWD
