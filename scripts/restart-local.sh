set -e
echo "~  restarting nara on $(hostname) ~"

echo "-> querying nara api..."
machines=$(curl --silent https://nara.eljojo.net/api.json|jq -r '.naras | map(.Name) | .[]')

for name in ${machines[@]}; do
  if systemctl show nara-$name > /dev/null; then
    systemctl restart nara-$name
    echo "restarted $name"
  fi
done

if systemctl show nara-web > /dev/null; then
  systemctl restart nara-web
  echo "restarted nara-web"
fi
