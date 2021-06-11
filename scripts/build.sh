#!/usr/bin/env bash
set -e
export PATH=$PATH:/usr/local/go/bin
cd "$(dirname "$(realpath "$0")")";

arch=$(uname -m)
if [ "$arch" == 'aarch64' ]; then
  echo "applying panicwrap linux hotfix lmao"
  if [ -z "$GOPATH" ]; then
    export GOPATH="$HOME/go"
  fi
  BUGFILE="$GOPATH/pkg/mod/github.com/bugsnag/panicwrap@v1.3.2/dup2.go"
  if [ -f "$BUGFILE" ]; then
    sudo rm $BUGFILE
  fi
fi
go build -o ../build/nara ../cmd/nara/main.go
echo "-> built nara on $(hostname)"
