#!/usr/bin/env bash
export PATH=$PATH:/usr/local/go/bin
cd "$(dirname "$(realpath "$0")")";

echo ""
echo "=> building nara on $(hostname)"
git pull
./get_deps.sh
echo "stopping nara *for build performance*"
sudo systemctl stop nara
echo "building"
go build nara.go
echo "restarting service"
sudo systemctl restart nara
echo "=> succesfully built nara on $(hostname)"
echo ""
echo ""
