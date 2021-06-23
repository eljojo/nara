'use strict';

function NaraRow(props) {
  const nara = props.nara;

  function timeAgo(a) {
    var difference_in_seconds = a;
    if (difference_in_seconds < 60) {
      difference_in_seconds = Math.round(difference_in_seconds/5) * 5
      return ("" + difference_in_seconds + "s");
    }
    const olderTime = (moment().unix() - a);
    return moment().to(olderTime * 1000, true)
  }

  const uptime = nara.Online == "ONLINE" ? timeAgo(nara.LastSeen - nara.LastRestart) : nara.Online;

  const url = `http://${nara.Name}-api.nara.network`;
  const nameOrLink =  nara.Online == "ONLINE" ? (<a href={url} target="_blank">{ nara.Name }</a>) : nara.Name;

  return (
    <tr>
      <td>{ nara.LicensePlate } { nameOrLink }</td>
      <td>{ nara.Flair }</td>
      <td>{ nara.Buzz  }</td>
      <td>{ nara.Chattiness  }</td>
      <td>{ timeAgo(moment().unix() - nara.LastSeen) } ago</td>
      <td>{ uptime }</td>
      <td>{ timeAgo(nara.LastSeen - nara.StartTime) }</td>
      <td>{ timeAgo(nara.Uptime)  }</td>
      <td>{ nara.Restarts }</td>
    </tr>
  );
}

function NaraList() {
  const { useState, useEffect } = React;
  const [data, setData] = useState({naras: [], server: 'unknown' });

  useEffect(() => {
    var lastDate = 0;

    const refresh = () => {
      const fetchDate = new Date;

      window.fetch("/narae.json")
        .then(response => response.json())
        .then(function(data) {
          if(fetchDate > lastDate) {
            setData(data);
            lastDate = fetchDate;
          }
        });
    };
    refresh();
    const interval = setInterval(refresh, 2000);
    return () => clearInterval(interval);
  }, []);

  return (
    <div>
      <table id="naras">
        <thead>
          <tr>
            <th>Name</th>
            <th>Flair</th>
            <th>Buzz</th>
            <th>Chat</th>
            <th>Last Ping</th>
            <th>Nara Uptime</th>
            <th>Nara Lifetime</th>
            <th>Host Uptime</th>
            <th>Restarts</th>
          </tr>
        </thead>
        <tbody>{
          data.naras.map((nara) =>
          <NaraRow nara={nara} key={nara.Name} />
        )
        }</tbody>
      </table>
    <span>rendered by { data.server }</span>
    </div>
  );
}

const domContainer = document.querySelector('#naras_container');
ReactDOM.render(React.createElement(NaraList), domContainer);
