#!/bin/sh

echo sending the PUT to reset all keys...
curl -X PUT localhost:8090/resetallkeys
