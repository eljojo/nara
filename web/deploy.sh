git push

deployMachines=(lisa.eljojo.casa bloo.eljojo.dev ned.eljojo.dev calle.eljojo.dev scl.eljojo.dev)

echo "deploying nara-web..."

NARA_VERSION=$(git rev-parse HEAD)

git push deploy -f "master:deploy"

for m in ${deployMachines[@]}; do
  echo "\n\n$m:"
  ssh $m "cd ~/nara && git checkout -f $NARA_VERSION && sudo systemctl restart nara-web"
done

echo "deploy finished, congrats :-)"
