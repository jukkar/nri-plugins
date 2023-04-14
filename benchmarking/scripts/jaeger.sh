#!/usr/bin/env bash

ENVVARS="-e COLLECTOR_ZIPKIN_HTTP_PORT=9411 \
         -e COLLECTOR_OTLP_ENABLED=true"
PORTS="-p 5775:5775/udp \
       -p 6831:6831/udp \
       -p 6832:6832/udp \
       -p 5778:5778 \
       -p 16686:16686 \
       -p 14268:14268 \
       -p 9411:9411 \
       -p 4317:4317 \
       -p 4318:4318"

if [ "$1" = "--permanent" ]; then
    storage=/tmp/jaeger-trace
    data=$storage/data
    key=$storage/key
    echo "Using $data and $key to store (badger) traces..."
    mkdir -p $storage
    STORAGE="-e SPAN_STORAGE_TYPE=badger \
      -e BADGER_EPHEMERAL=false \
      -e BADGER_DIRECTORY_VALUE=$data \
      -e BADGER_DIRECTORY_KEY=$key \
      -v $storage:$storage"
fi

cmd="docker run $ENVVARS $PORTS $STORAGE jaegertracing/all-in-one:latest"
echo "Running command $cmd..."
$cmd
