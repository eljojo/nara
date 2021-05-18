'use strict';

function NaraRow(props) {
  const nara = props.nara;
  const ver = nara.Verdict

  return (
    <tr>
      <td>{ nara.Name }</td>
      <td>{ nara.Barrio }</td>
      <td>{ nara.Verdict.Online }</td>
      <td>{ moment(ver.LastSeen * 1000).fromNow() }</td>
      <td>{ moment(ver.LastRestart * 1000).fromNow(true) }</td>
      <td>{ moment(ver.StartTime * 1000).fromNow(true) }</td>
      <td>{ ver.Restarts }</td>
      <td>{ moment().to((moment().unix() - nara.HostStats.Uptime) * 1000, true)  }</td>
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
