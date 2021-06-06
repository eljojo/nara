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

  return (
    <tr>
      <td>{ nara.Name }</td>
      <td>{ nara.Barrio }</td>
      <td>{ ver.Online }</td>
      <td>{ timeAgo(moment().unix() - ver.LastSeen) } ago</td>
      <td>{ timeAgo(ver.LastSeen - ver.LastRestart) }</td>
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
    const refresh = () => {
      window.fetch("/api.json")
        .then(response => response.json())
        .then(setData);
    };
    refresh();
    const interval = setInterval(refresh, 1000);
    return () => clearInterval(interval);
  }, []);

  return (
    <div>
      <table id="naras">
        <thead>
          <tr>
            <th>Name</th>
            <th>Neighbourhood</th>
            <th>Nara</th>
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
