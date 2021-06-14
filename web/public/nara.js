'use strict';

function NaraRow(props) {
  const nara = props.nara;
  const ver = nara.Observations[nara.Name]

  function timeAgo(a) {
    const difference_in_seconds = a;
    if (difference_in_seconds < 60) {
      return ("" + difference_in_seconds + "s");
    }
    const olderTime = (moment().unix() - a);
    return moment().to(olderTime * 1000, true)
  }

  const uptime = ver.Online == "ONLINE" ? timeAgo(ver.LastSeen - ver.LastRestart) : ver.Online;

  return (
    <tr>
      <td>{ nara.Name }</td>
      <td>{ nara.Flair }</td>
      <td>{ nara.Buzz  }</td>
      <td>{ nara.Chattiness  }</td>
      <td>{ timeAgo(moment().unix() - ver.LastSeen) } ago</td>
      <td>{ uptime }</td>
      <td>{ timeAgo(ver.LastSeen - ver.StartTime) }</td>
      <td>{ ver.Restarts }</td>
      <td>{ timeAgo(nara.HostStats.Uptime)  }</td>
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

      window.fetch("/api.json")
        .then(response => response.json())
        .then(function(data) {
          if(fetchDate > lastDate) {
            setData(data);
            lastDate = fetchDate;
          }
        });
    };
    refresh();
    const interval = setInterval(refresh, 500);
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
            <th>Restarts</th>
            <th>Host Uptime</th>
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
