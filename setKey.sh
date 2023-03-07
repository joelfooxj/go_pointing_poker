#!/bin/sh

echo sending the POST to set key...
curl -X POST localhost:8090/setkey \
-H "Content-Type: application/json" \
-d '{"key":"key1", "value":123}'
