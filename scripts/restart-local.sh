set -e
echo "~  restarting nara on $(hostname) ~"

for name in "$@"; do
  if systemctl is-enabled --quiet nara-$name 2> /dev/null; then
    systemctl restart nara-$name
    echo "restarted $name"
  fi
done
