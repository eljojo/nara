#!/usr/bin/env bash
export PATH=$PATH:/usr/local/go/bin
cd "$(dirname "$(realpath "$0")")";

echo ""
echo "=> deploying nara on $(hostname)"
./get_deps.sh
echo "[$(hostname)] building"
go build
echo "[$(hostname)] restarting service"
sudo systemctl restart nara
echo "=> succesfully deployed nara on $(hostname)"
echo ""
echo ""
