#!/bin/bash
export PATH=$PATH:/usr/local/go/bin
cd "$(dirname "$(realpath "$0")")";

git pull
./get_deps.sh
go build nara.go
sudo systemctl restart nara
