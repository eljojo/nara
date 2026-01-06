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

// Sparkle particle component
function Sparkle({ x, y, color, delay }) {
  const style = {
    left: x,
    top: y,
    animationDelay: `${delay}ms`,
  };
  return <div className={`sparkle ${color}`} style={style} />;
}

// Shooting Star Component with sparkles
function ShootingStar({ event, onComplete }) {
  const { useEffect, useState, useRef } = React;
  const [sparkles, setSparkles] = useState([]);
  const sparkleId = useRef(0);

  // Random starting Y position (top 60% of screen)
  const startY = Math.random() * 60 + 10; // 10% to 70%

  useEffect(() => {
    // Remove after animation completes
    const timer = setTimeout(() => {
      onComplete();
    }, 2500);

    // Generate sparkles along the path
    const colors = ['gold', 'pink', 'white', 'purple'];
    const sparkleInterval = setInterval(() => {
      // Calculate approximate current position based on time
      const elapsed = Date.now() % 2000;
      const progress = elapsed / 2000;

      // Only spawn sparkles during the visible part of animation
      if (progress > 0.1 && progress < 0.8) {
        const x = progress * (window.innerWidth + 200) - 100;
        const y = (startY / 100) * window.innerHeight - (progress * 80);

        // Add some randomness
        const offsetX = (Math.random() - 0.5) * 40;
        const offsetY = (Math.random() - 0.5) * 30;

        const newSparkle = {
          id: sparkleId.current++,
          x: x + offsetX,
          y: y + offsetY,
          color: colors[Math.floor(Math.random() * colors.length)],
          delay: Math.random() * 50,
        };

        setSparkles(prev => [...prev.slice(-6), newSparkle]); // Keep last 6 sparkles
      }
    }, 150);

    return () => {
      clearTimeout(timer);
      clearInterval(sparkleInterval);
    };
  }, [startY]);

  const style = {
    top: `${startY}%`,
    left: '-100px',
  };

  return (
    <React.Fragment>
      {sparkles.map(s => (
        <Sparkle key={s.id} x={s.x} y={s.y} color={s.color} delay={s.delay} />
      ))}
      <div className="shooting-star burst" style={style}>
        <span className="actor">{event.actor}</span>
        <span className="arrow">→</span>
        <span className="target">{event.target}</span>
        <span style={{ marginLeft: '8px', opacity: 0.9 }}>{event.message}</span>
      </div>
    </React.Fragment>
  );
}

// Container for all shooting stars
function ShootingStarContainer() {
  const { useState, useEffect, useRef } = React;
  const [stars, setStars] = useState([]);
  const lastStarTime = useRef(0);

  // Rate limit: minimum 2.5-5 minutes between stars (randomized)
  const MIN_INTERVAL_MS = 150000; // 2.5 minutes
  const MAX_INTERVAL_MS = 300000; // 5 minutes

  const maybeAddStar = (event) => {
    const now = Date.now();
    const timeSinceLast = now - lastStarTime.current;
    // Randomize the cooldown between min and max
    const cooldown = MIN_INTERVAL_MS + Math.random() * (MAX_INTERVAL_MS - MIN_INTERVAL_MS);

    if (timeSinceLast < cooldown) {
      return; // Too soon, skip this one
    }

    lastStarTime.current = now;
    const id = now + Math.random();
    setStars(prev => [...prev, { id, event }]);
  };

  useEffect(() => {
    // Connect to SSE endpoint
    const eventSource = new EventSource('/events');

    eventSource.addEventListener('social', (e) => {
      const event = JSON.parse(e.data);
      maybeAddStar(event);
    });

    return () => {
      eventSource.close();
    };
  }, []);

  const removeStar = (id) => {
    setStars(prev => prev.filter(s => s.id !== id));
  };

  return (
    <div className="shooting-star-container">
      {stars.map(({ id, event }) => (
        <ShootingStar key={id} event={event} onComplete={() => removeStar(id)} />
      ))}
    </div>
  );
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

  const nameOrLink = (nara.Online == "ONLINE" && nara.PublicUrl)
    ? (<a href={nara.PublicUrl} target="_blank">{ nara.Name }</a>)
    : nara.Name;

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

// Social panel showing opinions and recent teases
function SocialPanel() {
  const { useState, useEffect } = React;
  const [clout, setClout] = useState({});
  const [recentEvents, setRecentEvents] = useState([]);
  const [server, setServer] = useState('');

  function timeAgo(timestamp) {
    const seconds = moment().unix() - timestamp;
    if (seconds < 60) {
      return `${Math.round(seconds)}s ago`;
    }
    if (seconds < 3600) {
      return `${Math.round(seconds / 60)}m ago`;
    }
    if (seconds < 86400) {
      return `${Math.round(seconds / 3600)}h ago`;
    }
    return `${Math.round(seconds / 86400)}d ago`;
  }

  useEffect(() => {
    const refresh = () => {
      window.fetch("/social/clout")
        .then(response => response.json())
        .then(data => {
          setClout(data.clout || {});
          setServer(data.server);
        })
        .catch(() => {});

      window.fetch("/social/recent")
        .then(response => response.json())
        .then(data => {
          setRecentEvents(data.events || []);
        })
        .catch(() => {});
    };

    refresh();
    const interval = setInterval(refresh, 10000); // every 10s instead of 2s
    return () => clearInterval(interval);
  }, []);

  // Sort clout by score descending
  const sortedClout = Object.entries(clout)
    .sort((a, b) => b[1] - a[1])
    .slice(0, 10);

  const cloutEmoji = (score) => {
    if (score >= 8) return '🌟';
    if (score >= 5) return '✨';
    if (score >= 2) return '👍';
    if (score >= 0) return '😐';
    return '👎';
  };

  return (
    <div style={{ display: 'flex', gap: '20px', marginBottom: '20px' }}>
      <div style={{ flex: 1, padding: '10px', border: '1px solid #ddd', borderRadius: '8px' }}>
        <strong>💭 {server}'s Opinions:</strong>
        {sortedClout.length === 0 && <div style={{ color: '#888', marginTop: '8px' }}>No opinions yet...</div>}
        <div style={{ marginTop: '8px' }}>
          {sortedClout.map(([name, score]) => (
            <div key={name} style={{ display: 'flex', justifyContent: 'space-between', padding: '2px 0' }}>
              <span>{name}</span>
              <span>{cloutEmoji(score)} {score.toFixed(1)}</span>
            </div>
          ))}
        </div>
      </div>
      <div style={{ flex: 1, padding: '10px', border: '1px solid #ddd', borderRadius: '8px' }}>
        <strong>📜 Recent Teases:</strong>
        {recentEvents.length === 0 && <div style={{ color: '#888', marginTop: '8px' }}>No teases yet...</div>}
        <div style={{ marginTop: '8px' }}>
          {recentEvents.map((event, i) => (
            <div key={i} style={{ padding: '4px 0', borderBottom: i < recentEvents.length - 1 ? '1px solid #eee' : 'none' }}>
              <div style={{ fontSize: '0.9em' }}>
                <strong>{event.actor}</strong> → <strong>{event.target}</strong>
                <span style={{ color: '#888', marginLeft: '8px' }}>{timeAgo(event.timestamp)}</span>
              </div>
              <div style={{ fontSize: '0.85em', color: '#666', fontStyle: 'italic' }}>"{event.message}"</div>
            </div>
          ))}
        </div>
      </div>
    </div>
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
              .sort((a, b) => a.Name.localeCompare(b.Name));
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
      <SocialPanel />
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

// World Journey Panel
function WorldJourneyPanel() {
  const { useState, useEffect } = React;
  const [message, setMessage] = useState('');
  const [journeys, setJourneys] = useState([]);
  const [sending, setSending] = useState(false);
  const [error, setError] = useState('');

  const fetchJourneys = () => {
    window.fetch("/world/journeys")
      .then(response => response.json())
      .then(data => {
        setJourneys(data.journeys || []);
      })
      .catch(() => {});
  };

  useEffect(() => {
    fetchJourneys();
    const interval = setInterval(fetchJourneys, 5000);
    return () => clearInterval(interval);
  }, []);

  const startJourney = () => {
    if (!message.trim()) return;
    setSending(true);
    setError('');

    window.fetch("/world/start", {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ message: message.trim() })
    })
      .then(response => {
        if (!response.ok) {
          return response.text().then(text => { throw new Error(text); });
        }
        return response.json();
      })
      .then(() => {
        setMessage('');
        fetchJourneys();
      })
      .catch(err => {
        setError(err.message);
      })
      .finally(() => {
        setSending(false);
      });
  };

  const formatTime = (timestamp) => {
    return moment.unix(timestamp).format('HH:mm:ss');
  };

  return (
    <div style={{ marginBottom: '20px', padding: '15px', border: '2px solid #667eea', borderRadius: '12px', background: 'linear-gradient(135deg, #f5f7fa 0%, #e4e8f0 100%)' }}>
      <h3 style={{ margin: '0 0 15px 0', color: '#667eea' }}>Going Around the World</h3>

      <div style={{ display: 'flex', gap: '10px', marginBottom: '15px' }}>
        <input
          type="text"
          value={message}
          onChange={(e) => setMessage(e.target.value)}
          onKeyPress={(e) => e.key === 'Enter' && startJourney()}
          placeholder="Enter a message to send around the world..."
          style={{
            flex: 1,
            padding: '10px 15px',
            borderRadius: '8px',
            border: '1px solid #ddd',
            fontSize: '14px'
          }}
          disabled={sending}
        />
        <button
          onClick={startJourney}
          disabled={sending || !message.trim()}
          style={{
            padding: '10px 20px',
            borderRadius: '8px',
            border: 'none',
            background: sending ? '#ccc' : 'linear-gradient(135deg, #667eea 0%, #764ba2 100%)',
            color: 'white',
            fontWeight: 'bold',
            cursor: sending ? 'not-allowed' : 'pointer'
          }}
        >
          {sending ? 'Sending...' : 'Send'}
        </button>
      </div>

      {error && (
        <div style={{ color: 'red', marginBottom: '10px', fontSize: '14px' }}>{error}</div>
      )}

      <div>
        <strong>Completed Journeys:</strong>
        {journeys.length === 0 && (
          <div style={{ color: '#888', marginTop: '10px', fontStyle: 'italic' }}>
            No completed journeys yet. Start one above!
          </div>
        )}

        {journeys.map((journey, idx) => (
          <JourneyReceipt key={journey.id || idx} journey={journey} />
        ))}
      </div>
    </div>
  );
}

// Journey Receipt Component
function JourneyReceipt({ journey }) {
  const formatTime = (timestamp) => {
    return moment.unix(timestamp).format('MMM D, HH:mm:ss');
  };

  const totalTime = journey.hops.length > 0
    ? journey.hops[journey.hops.length - 1].timestamp - journey.hops[0].timestamp
    : 0;

  return (
    <div style={{
      marginTop: '10px',
      padding: '12px',
      background: 'white',
      borderRadius: '8px',
      border: '1px solid #e0e0e0',
      boxShadow: '0 2px 4px rgba(0,0,0,0.05)'
    }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start' }}>
        <div>
          <div style={{ fontWeight: 'bold', fontSize: '15px', marginBottom: '5px' }}>
            "{journey.message}"
          </div>
          <div style={{ fontSize: '12px', color: '#666' }}>
            Started by <strong>{journey.originator}</strong>
          </div>
        </div>
        {journey.complete && (
          <span style={{
            background: 'linear-gradient(135deg, #11998e 0%, #38ef7d 100%)',
            color: 'white',
            padding: '3px 10px',
            borderRadius: '12px',
            fontSize: '11px',
            fontWeight: 'bold'
          }}>
            COMPLETE
          </span>
        )}
      </div>

      <div style={{ marginTop: '10px', display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: '5px' }}>
        <span style={{ fontSize: '13px', color: '#444' }}>{journey.originator}</span>
        {journey.hops.map((hop, i) => (
          <React.Fragment key={i}>
            <span style={{ color: '#999' }}>→</span>
            <span style={{ fontSize: '13px' }}>
              <span style={{ marginRight: '2px' }}>{hop.stamp}</span>
              <strong>{hop.nara}</strong>
            </span>
          </React.Fragment>
        ))}
      </div>

      {journey.complete && (
        <div style={{ marginTop: '10px', paddingTop: '10px', borderTop: '1px dashed #e0e0e0' }}>
          <div style={{ fontSize: '12px', color: '#666', marginBottom: '5px' }}>
            <strong>Rewards:</strong>
          </div>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: '8px' }}>
            {Object.entries(journey.rewards || {}).map(([nara, clout]) => (
              <span key={nara} style={{
                background: clout >= 10 ? '#ffd700' : '#e8e8e8',
                padding: '2px 8px',
                borderRadius: '10px',
                fontSize: '11px'
              }}>
                {nara}: +{clout}
              </span>
            ))}
          </div>
          <div style={{ fontSize: '11px', color: '#999', marginTop: '5px' }}>
            Total time: {totalTime}s
          </div>
        </div>
      )}
    </div>
  );
}

// Main App with shooting stars
function App() {
  return (
    <React.Fragment>
      <ShootingStarContainer />
      <WorldJourneyPanel />
      <NaraList />
    </React.Fragment>
  );
}

const domContainer = document.querySelector('#naras_container');
ReactDOM.render(React.createElement(App), domContainer);
