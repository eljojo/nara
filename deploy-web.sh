set -x

NARA_VERSION=$(git rev-parse --short HEAD)
echo "~  deploying nara version $NARA_VERSION ~"

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

if ! grep -Fq 'remote "deploy"' .git/config; then # naive check to see there's a deploy upstream
  git remote add deploy jojo@lisa.eljojo.casa:nara
fi

for name in ${machines[@]}; do
  m=$(naraSsh $name)
  if ! grep -Eq "pushurl.+$name" .git/config; then # naive check to see if in deploy upstream
    nslookup "$name.eljojo.dev" || ((echo "# skip pushurl $name") >> .git/config && continue)
    git remote set-url --add --push deploy $m:nara
  fi
done

echo "pushing code to all machines"
git push deploy -f "HEAD:deploy" -q

for name in ${machines[@]}; do
  m=$(naraSsh $name)

  nslookup "$name.eljojo.dev" || (echo "skipping $name" && continue)

  if [[ "$name" != "music-station" &&  "$name" != "music-pi" &&  "$name" != "cayumanqui" &&  "$name" != "desk-pi" ]]; then
    echo "=> deploying nara-web on $name"
    ssh -q $m "cd ~/nara && git checkout -f $NARA_VERSION -q"
    ssh -q $m "sudo systemctl restart nara-web"
  fi
done

echo "nara deployed 🎉 congrats :-)"
