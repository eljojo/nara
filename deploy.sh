git push

deployMachines=(dietpi@desk-pi.eljojo.dev lisa.eljojo.casa bloo.eljojo.net dietpi@music-station.eljojo.dev dietpi@cayumanqui.eljojo.dev ned.eljojo.net  dietpi@music-pi.eljojo.dev  pi@inky.eljojo.dev)

echo "deploying nara..."

for m in ${deployMachines[@]}; do
  ssh $m "cd ~/nara && git checkout -f master"
done

git push deploy -f master:deploy

for m in ${deployMachines[@]}; do
  echo "\n\n$m:"
  ssh $m "cd ~/nara && git checkout -f deploy"
  ssh $m '~/nara/build.sh'
  echo "sleeping for 65 seconds to stabilize narae memory"
  sleep 65
done
