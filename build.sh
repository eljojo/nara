#!/usr/bin/env bash
export PATH=$PATH:/usr/local/go/bin
cd "$(dirname "$(realpath "$0")")";

echo "-> local build on $(hostname)"
go build
echo "-> restarting service"
sudo systemctl restart nara
echo "=> succesfully deployed nara on $(hostname)"
