git push

deployMachines=(dietpi@desk-pi.eljojo.dev lisa.eljojo.casa bloo.eljojo.dev dietpi@music-station.eljojo.dev dietpi@cayumanqui.eljojo.dev ned.eljojo.dev calee.eljojo.dev calle.eljojo.dev dietpi@music-pi.eljojo.dev fritz.eljojo.dev shinagawa.eljojo.dev)

echo "deploying nara..."

NARA_VERSION=$(git rev-parse HEAD)

git push deploy -f "master:deploy"

for m in ${deployMachines[@]}; do
  echo "\n\n$m:"
  ssh $m "cd ~/nara && git checkout -f $NARA_VERSION"
  ssh $m '~/nara/build.sh'
  echo "sleeping for 20 seconds to stabilize narae memory"
  sleep 20
done

echo "deploy finished, congrats :-)"
