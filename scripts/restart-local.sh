echo "~  restarting nara on $(hostname) ~"

echo "-> querying nara api..."
machines=$(curl --silent https://nara.eljojo.net/api.json|jq -r '.naras | map(.Name) | .[]')

for name in ${machines[@]}; do
  systemctl restart nara-$name
  if [[ "$name" != "music-station" &&  "$name" != "music-pi" &&  "$name" != "cayumanqui" &&  "$name" != "desk-pi" ]]; then
    systemctl restart nara-web
  fi
done
