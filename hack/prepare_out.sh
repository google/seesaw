#!/bin/bash

mkdir -p "${GOPATH}/bin/out/"

for component in {ecu,engine,ha,healthcheck,ncc,watchdog}; do
    install "/go/bin/seesaw_${component}" "/go/bin/out/"
done
