set -e
echo "~  restarting nara on $(hostname) ~"

for name in "$@"; do
  if systemctl is-enabled --quiet nara-$name; then
    systemctl restart nara-$name
    echo "restarted $name"
  fi
done

if systemctl is-enabled --quiet nara-web; then
  systemctl restart nara-web
  echo "restarted nara-web"
fi
