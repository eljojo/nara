#!/bin/bash
export PATH=$PATH:/usr/local/go/bin
cd "$(dirname "$(realpath "$0")")";

echo "=> building nara on $(hostname)"
git pull
./get_deps.sh
echo "building"
go build nara.go
echo "restarting service"
sudo systemctl restart nara
