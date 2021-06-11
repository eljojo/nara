set -e
echo "~  restarting nara on $(hostname) ~"

machines=$(curl --silent https://nara.eljojo.net/api.json|jq -r '.naras | map(.Name) | .[]')

for name in ${machines[@]}; do
  if systemctl is-enabled --quiet nara-$name; then
    systemctl restart nara-$name
    echo "restarted $name"
  fi
done

if systemctl is-enabled --quiet nara-web; then
  systemctl restart nara-web
  echo "restarted nara-web"
fi
