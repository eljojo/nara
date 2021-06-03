set -e

NARA_VERSION=$(git rev-parse --short HEAD)
echo "~  deploying nara-web version $NARA_VERSION ~"
git push -q

#deployMachines=(lisa.eljojo.casa bloo.eljojo.dev ned.eljojo.dev calle.eljojo.dev calee.eljojo.dev shinagawa.eljojo.dev fritz.eljojo.dev)

echo "-> querying nara api..."
machines=$(curl --silent https://nara.eljojo.net/api.json|jq -r '.naras | map(.Name) | .[]')

git push deploy -f "master:deploy"

for name in ${machines[@]}; do
  if [[ "$name" == "music-station" ||  "$name" == "music-pi" ||  "$name" == "cayumanqui" ||  "$name" == "desk-pi" ]]; then
    continue
  fi

  m="$name.eljojo.dev"
  echo "=> deploying nara-web on $m"
  ssh $m "cd ~/nara && git checkout -f $NARA_VERSION -q && sudo systemctl restart nara-web"
done

echo "deploy finished, congrats :-)"
