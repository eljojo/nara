'use strict';

const e = React.createElement;

class NaraList extends React.Component {
  constructor(props) {
    super(props);
    // this.state = { Name: 'jojo', HostStats: {} };
  }

  render() {
    const renderNara = function(name, nara) {
      return (
        <tr>
        <td>{ name }</td>
        <td>{ nara.Barrio }</td>
        <td>{ nara.Verdict.Online }</td>
        <td>{ moment(nara.Verdict.LastSeen * 1000).fromNow() }</td>
        <td>{ moment(nara.Verdict.LastRestart * 1000).fromNow(true) }</td>
        <td>{ moment(nara.Verdict.StartTime * 1000).fromNow(true) }</td>
        <td>{ nara.Verdict.Restarts }</td>
        <td>{ moment().to((moment().unix() - nara.HostStats.Uptime) * 1000, true)  }</td>
        </tr>
      );
    }

    const naraData = this.props.naras;
    const naras = Object.keys(naraData).map((name, index) =>
      renderNara(name, naraData[name])
    );

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
        <tbody>{ naras }</tbody>
      </table>
    );
  }
}

const domContainer = document.querySelector('#naras_container');
var updateNaras = function(data) {
  ReactDOM.render(e(NaraList, { naras: data }), domContainer);
};
var refreshNow = function() {
  fetch('/api.json')
    .then(response => response.json())
    .then(updateNaras);
};
refreshNow();
setInterval(refreshNow, 1000);
