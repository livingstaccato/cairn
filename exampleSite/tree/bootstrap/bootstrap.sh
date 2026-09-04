#!/bin/sh
set -eu
curl -fsS "$MIRROR/bootstrap/cloud-init.yaml" -o /tmp/cloud-init.yaml
