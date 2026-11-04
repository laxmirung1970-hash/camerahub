#!/bin/sh
set -eu
umask 077
mkdir -p /data
chown 10001:10001 /data
chmod 700 /data
exec su-exec 10001:10001 /usr/local/bin/camerahub
