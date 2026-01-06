'use strict';

function stringToColor(str) {
  var hash = 0;
  for (var i = 0; i < str.length; i++) {
    hash = str.charCodeAt(i) + ((hash << 5) - hash);
  }
  var colour = '#';
  for (var i = 0; i < 3; i++) {
    var value = (hash >> (i * 8)) & 0xFF;
    colour += ('00' + value.toString(16)).substr(-2);
  }
  return colour;
}

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

  const url = `https://${nara.Name}.nara.network`;
  const nameOrLink =  nara.Online == "ONLINE" ? (<a href={url} target="_blank">{ nara.Name }</a>) : nara.Name;

  const trendColor = nara.Trend ? stringToColor(nara.Trend) : 'transparent';
  const trendStyle = {
    backgroundColor: trendColor,
    color: nara.Trend ? 'white' : 'black',
    padding: '2px 6px',
    borderRadius: '4px',
    fontSize: '0.8em',
    fontWeight: 'bold'
  };
  const trend = nara.Trend ? (<span style={trendStyle}>{nara.TrendEmoji} {nara.Trend}</span>) : "";

  return (
    <tr>
      <td>{ nara.LicensePlate } { nameOrLink }</td>
      <td>{ nara.Flair }</td>
      <td>{ nara.Buzz  }</td>
      <td>{ trend }</td>
      <td>{ nara.Chattiness  }</td>
      <td>{ timeAgo(moment().unix() - nara.LastSeen) } ago</td>
      <td>{ uptime }</td>
      <td>{ timeAgo(nara.LastSeen - nara.StartTime) }</td>
      <td>{ timeAgo(nara.Uptime)  }</td>
      <td>{ nara.Restarts }</td>
    </tr>
  );
}

function TrendSummary({ naras }) {
  const trends = naras.reduce((acc, nara) => {
    if (nara.Trend) {
      acc[nara.Trend] = (acc[nara.Trend] || 0) + 1;
    }
    return acc;
  }, {});

  return (
    <div style={{ marginBottom: '20px', padding: '10px', border: '1px solid #ddd', borderRadius: '8px' }}>
      <strong>Current Trends:</strong>
      {Object.entries(trends).length === 0 && <span> None at the moment</span>}
      {Object.entries(trends).map(([trend, count]) => (
        <span key={trend} style={{ marginLeft: '10px', padding: '4px 8px', backgroundColor: stringToColor(trend), color: 'white', borderRadius: '12px', fontSize: '0.9em' }}>{trend} ({count})</span>
      ))}
    </div>
  );
}

function NaraList() {
  const { useState, useEffect } = React;
  const [data, setData] = useState({filteredNaras: [], server: 'unknown' });
  const yesterday = moment().subtract(1, 'days').unix()

  useEffect(() => {
    var lastDate = 0;

    const refresh = () => {
      const fetchDate = new Date;

      window.fetch("/narae.json")
        .then(response => response.json())
        .then(function(data) {
          if(fetchDate > lastDate) {
            const filteredNaras = data.naras
              .filter(nara => nara.LastSeen > yesterday)
              .sort((a, b) => b.LastSeen - a.LastSeen);
            const newData = Object.assign(data, { filteredNaras: filteredNaras });
            setData(newData);
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
      <TrendSummary naras={data.filteredNaras} />
      <table id="naras">
        <thead>
          <tr>
            <th>Name</th>
            <th>Flair</th>
            <th>Buzz</th>
            <th>Trend</th>
            <th>Chat</th>
            <th>Last Ping</th>
            <th>Nara Uptime</th>
            <th>Nara Lifetime</th>
            <th>Host Uptime</th>
            <th>Restarts</th>
          </tr>
        </thead>
        <tbody>{
          data.filteredNaras.map((nara) =>
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
