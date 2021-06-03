NARA_VERSION=$(git rev-parse --short HEAD)
echo "~  deploying nara version $NARA_VERSION ~"

git push -q

#deployMachines=(dietpi@desk-pi.eljojo.dev lisa.eljojo.casa bloo.eljojo.dev dietpi@music-station.eljojo.dev dietpi@cayumanqui.eljojo.dev ned.eljojo.dev calee.eljojo.dev calle.eljojo.dev dietpi@music-pi.eljojo.dev fritz.eljojo.dev shinagawa.eljojo.dev)

echo "-> querying nara api..."
machines=$(curl --silent https://nara.eljojo.net/api.json|jq -r '.naras | map(.Name) | .[]')
echo "deploying to" $machines

naraSsh () {
  local m="$1.eljojo.dev"
  if [[ "$1" == "music-station" ||  "$1" == "music-pi" ||  "$1" == "cayumanqui" ||  "$1" == "desk-pi" ]]; then
    m="dietpi@$m"
  else
    m="jojo@$m"
  fi

  echo "$m"
}

for name in ${machines[@]}; do
  m=$(naraSsh $name)
  if ! grep -Fq $name .git/config; then # naive check to see if in deploy upstream
    echo "==> adding remote for $m"
    git remote set-url --add --push deploy $m:nara
  fi
done

git push deploy -f "master:deploy" -q

#for m in ${deployMachines[@]}; do
for name in ${machines[@]}; do
  m=$(naraSsh $name)
  echo "=> deploying nara on $name"
  ssh $m "cd ~/nara && git checkout -f $NARA_VERSION -q"
  ssh $m '~/nara/build.sh'
  echo "sleeping for 20 seconds to stabilize narae memory"
  sleep 20
done

echo "deploy finished 🎉 congrats :-)"
