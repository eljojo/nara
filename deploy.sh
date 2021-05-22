git push

# deployMachines=(dietpi@desk-pi.eljojo.dev lisa.eljojo.casa bloo.eljojo.dev dietpi@music-station.eljojo.dev dietpi@cayumanqui.eljojo.dev ned.eljojo.net  dietpi@music-pi.eljojo.dev  pi@inky.eljojo.dev)
deployMachines=(dietpi@desk-pi.eljojo.dev lisa.eljojo.casa bloo.eljojo.dev dietpi@music-station.eljojo.dev dietpi@cayumanqui.eljojo.dev ned.eljojo.dev scl.eljojo.dev calle.eljojo.dev dietpi@music-pi.eljojo.dev)

echo "deploying nara..."

NARA_VERSION=$(git rev-parse HEAD)

git push deploy -f "master:deploy"

for m in ${deployMachines[@]}; do
  echo "\n\n$m:"
  ssh $m "cd ~/nara && git checkout -f $NARA_VERSION"
  ssh $m '~/nara/build.sh'
  echo "sleeping for 40 seconds to stabilize narae memory"
  sleep 40
done

echo "deploy finished, congrats :-)"
