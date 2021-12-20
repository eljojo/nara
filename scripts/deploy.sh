set -e

NARA_VERSION=$(git rev-parse --short HEAD)
echo "~  deploying nara version $NARA_VERSION ~"

echo "-> querying nara api..."
machines=$(curl --silent https://nara.eljojo.net/narae.json|jq -r '.naras | map(.Name) | .[]')
echo "deploying to" $machines

naraSsh () {
  local m="$1.eljojo.dev"
  if [[ "$1" == "music-pi" ||  "$1" == "cayumanqui" ]]; then
    m="dietpi@$m"
  else
    m="jojo@$m"
  fi

  echo "$m"
}

if ! grep -Fq 'remote "deploy"' .git/config; then # naive check to see there's a deploy upstream
  git remote add deploy jojo@lisa.eljojo.casa:nara-web
fi

for name in ${machines[@]}; do
  m=$(naraSsh $name)
  if ! grep -Eq "pushurl.+$name" .git/config; then # naive check to see if in deploy upstream
    if nslookup "$name.eljojo.dev" > /dev/null; then
      git remote set-url --add --push deploy $m:nara-web
    else
      echo "# skip pushurl $name" >> .git/config
    fi
  fi
done

echo "pushing code to all machines"
git push deploy -f "HEAD:deploy" -q

for name in ${machines[@]}; do
  m=$(naraSsh $name)

  if ! nslookup "$name.eljojo.dev" > /dev/null; then
    echo "skipping $name" && continue
  fi

  if [[ "$name" != "music-pi" &&  "$name" != "cayumanqui" ]]; then
    echo "=> deploying nara-web on $name"
    ssh -q $m "cd ~/nara-web && git checkout -f $NARA_VERSION -q"
    ssh -q $m "sudo systemctl restart nara-web"
  fi
done

echo "nara deployed 🎉 congrats :-)"
