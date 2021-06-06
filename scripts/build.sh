#!/usr/bin/env bash
export PATH=$PATH:/usr/local/go/bin
cd "$(dirname "$(realpath "$0")")";

echo "-> local build on $(hostname)"
arch=$(uname -m)
if [ "$arch" == 'aarch64' ]; then
  echo "applying panicwrap linux hotfix lmao"
  sudo rm $HOME/go/pkg/mod/github.com/bugsnag/panicwrap@v1.3.2/dup2.go || true
fi
go build -o ../build/nara ..
