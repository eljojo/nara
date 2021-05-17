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
  const [data, setData] = useState([]);

  useEffect(() => {
    const refresh = () => {
      window.fetch("/api.json")
        .then(response => response.json())
        .then(root => {
          const data = Object.entries(root).map(item => (Object.assign(item[1], {
            Name: item[0],
          })));
          setData(data);
        });
    };
    refresh();
    const interval = setInterval(refresh, 1000);
    return () => clearInterval(interval);
  }, []);

  return (
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
        data.map((nara) =>
        <NaraRow nara={nara} key={nara.Name} />
      )
      }</tbody>
    </table>
  );
}

const domContainer = document.querySelector('#naras_container');
ReactDOM.render(e(NaraList), domContainer);
