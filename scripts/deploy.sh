set -e

NARA_VERSION=$(git rev-parse --short HEAD)
echo "~  deploying nara version $NARA_VERSION ~"

echo "-> querying nara api..."
machines=$(curl --silent https://nara.network/narae.json|jq -r '.naras | map(.Name) | .[]')
machines_in_one_line=$(echo "$machines"|tr '\n' ' ')

naraSsh () {
  local m="$1.eljojo.dev"
  if [[ "$1" == "cayumanqui" ]]; then
    m="dietpi@$m"
  else
    m="jojo@$m"
  fi

  echo "$m"
}

if grep -Fq 'remote "deploy"' .git/config; then # naive check to see there's a deploy upstream
  git remote remove deploy
fi
git remote add deploy jojo@lisa.eljojo.casa:nara

for name in ${machines[@]}; do
  m=$(naraSsh $name)
  if ! grep -Eq "pushurl.+$name" .git/config; then # naive check to see if in deploy upstream
    if nslookup "$name.eljojo.dev" > /dev/null; then
      git remote set-url --add --push deploy $m:nara
    else
      echo "# skip pushurl $name" >> .git/config
    fi
  fi
done

echo "pushing code to all machines"
git push deploy -f "HEAD:deploy" -q

for name in ${machines[@]}; do
  if [[ "$1" != "" ]]; then
    if [[ $(echo "$name" | grep -E "$1") == "" ]]; then
      echo "skipping $name"
      delete=$name
      machines=( "${machines[@]/$delete}" )
    fi
  fi
done

echo "deploying to" $machines
for name in ${machines[@]}; do
  m=$(naraSsh $name)

  if ! nslookup "$name.eljojo.dev" > /dev/null; then
    continue
  fi

  echo "=> deploying nara on $name"
  ssh -q $m "cd ~/nara && git checkout -f $NARA_VERSION -q"
  ssh -q $m '~/nara/scripts/build.sh'
  ssh -q $m "sudo ~/nara/scripts/restart-local.sh $machines_in_one_line"

  echo "=> succesfully deployed $name"
done

echo "nara deployed 🎉 congrats :-)"
