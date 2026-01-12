'use strict';
dayjs.extend(window.dayjs_plugin_relativeTime);


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

var AvatarCanvas = window.NaraAvatar ? window.NaraAvatar.AvatarCanvas : null;
var clamp01 = window.NaraAvatar ? window.NaraAvatar.clamp01 : function(v) {
  return Math.max(0, Math.min(1, v));
};

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
      const data = JSON.parse(e.data);
      if (data.social) {
        const event = {
          actor: data.social.actor,
          target: data.social.target,
          message: data.social.reason,
        };
        maybeAddStar(event);
      }
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
  const { useRef } = React;
  const pointerRef = useRef({ x: 0, y: 0, active: false });
  const isOnline = nara.Online === "ONLINE";

  function timeAgo(a, addAgo = false) {
    // Fix "20462 days ago" bug - return "never" for invalid values
    if (a <= 0 || !isFinite(a)) {
      return "never";
    }
    var difference_in_seconds = a;
    if (difference_in_seconds < 60) {
      difference_in_seconds = Math.round(difference_in_seconds/5) * 5
      return ("" + difference_in_seconds + "s") + (addAgo ? " ago" : "");
    }
    const olderTime = (dayjs().unix() - a);
    return dayjs().to(dayjs(olderTime * 1000), true) + (addAgo ? " ago" : "");
  }

  // Calculate uptime, but guard against invalid LastRestart (0 or missing)
  let uptime = nara.Online;
  if (nara.Online == "ONLINE") {
    const uptimeSeconds = nara.LastSeen - nara.LastRestart;
    // If LastRestart is 0 or uptime is > 1 year (clearly wrong), use nara lifetime with "?"
    if (nara.LastRestart === 0 || uptimeSeconds > 31536000) {
      const lifetime = nara.LastSeen - nara.StartTime;
      uptime = timeAgo(lifetime) + "?";
    } else {
      uptime = timeAgo(uptimeSeconds);
    }
  }

  const nameOrLink = (nara.Online == "ONLINE" && nara.PublicUrl)
    ? (<a href={nara.PublicUrl} target="_blank">{ nara.Name }</a>)
    : (<a href={`/nara/${encodeURIComponent(nara.Name)}`}>{ nara.Name }</a>);

  // Aura color dot with secondary border and dynamic glow
  const primaryColor = nara.Aura || '#888';
  const secondaryColor = nara.AuraSecondary || nara.Aura || '#666';
  const sociability = nara.Sociability || 50;
  const buzz = nara.Buzz || 0;
  const chill = nara.Chill || 50;
  const agreeableness = nara.Agreeableness || 50;

  // Glow intensity based on sociability (0-100 -> 2-8px blur)
  const glowBlur = 2 + (sociability / 100) * 6;

  // Buzz creates pulsing animation
  const pulseAnimation = buzz > 5 ? 'aura-pulse 2s ease-in-out infinite' : 'none';

  const auraStyle = {
    display: 'inline-block',
    width: '10px',
    height: '10px',
    borderRadius: '50%',
    backgroundColor: primaryColor,
    border: `1.5px solid ${secondaryColor}`,
    marginRight: '6px',
    boxShadow: `0 0 ${glowBlur}px ${primaryColor}`,
    verticalAlign: 'middle',
    animation: pulseAnimation
  };

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
      <td>
        <span
          style={{ display: 'inline-flex', alignItems: 'center', gap: '8px' }}
        >
          {isOnline && nara.ID && AvatarCanvas ? (
            <span
              style={{ display: 'inline-flex', width: '40px', height: '40px', alignItems: 'center', justifyContent: 'center' }}
              onMouseEnter={() => {
                pointerRef.current.active = true;
              }}
              onMouseMove={(e) => {
                var rect = e.currentTarget.getBoundingClientRect();
                var cx = rect.left + rect.width / 2;
                var cy = rect.top + rect.height / 2;
                var nx = (e.clientX - cx) / (rect.width / 2);
                var ny = (e.clientY - cy) / (rect.height / 2);
                pointerRef.current.x = clamp01(Math.abs(nx)) * Math.sign(nx);
                pointerRef.current.y = clamp01(Math.abs(ny)) * Math.sign(ny);
                pointerRef.current.active = true;
              }}
              onMouseLeave={() => {
                pointerRef.current.x = 0;
                pointerRef.current.y = 0;
                pointerRef.current.active = false;
              }}
            >
              <AvatarCanvas
                id={nara.ID}
                primary={primaryColor}
                secondary={secondaryColor}
                sociability={sociability}
                chill={chill}
                agreeableness={agreeableness}
                buzz={buzz}
                size={36}
                pointerRef={pointerRef}
              />
            </span>
          ) : (
            <span className="nara-avatar nara-avatar-placeholder" aria-hidden="true"></span>
          )}
          <span>{ nameOrLink }</span>
        </span>
      </td>
      <td>{ nara.LicensePlate }{ nara.Flair }</td>
      <td>{ nara.Buzz  }</td>
      <td>{ trend }</td>
      <td>{ nara.Chattiness  }</td>
      <td>{ (nara.LastSeen < 5 && nara.Online == "ONLINE") ? "just now" : timeAgo(dayjs().unix() - nara.LastSeen, true) }</td>
      <td>{ uptime }</td>
      <td>{ timeAgo(nara.LastSeen - nara.StartTime, true) }</td>
      <td>{ timeAgo(nara.Uptime)  }</td>
      <td>{ nara.Restarts }</td>
    </tr>
  );
}

// Social panel showing opinions and network activity
function SocialPanel() {
  const { useState, useEffect } = React;
  const [clout, setClout] = useState({});
  const [recentEvents, setRecentEvents] = useState([]);
  const [server, setServer] = useState('');
  const [naraColors, setNaraColors] = useState({}); // Map of nara names to aura colors

  function timeAgo(timestamp) {
    // Fix "20462 days ago" bug - return "never" for zero/invalid timestamps
    if (timestamp === 0 || timestamp < 0) {
      return "never";
    }
    // Convert from nanoseconds to seconds if needed
    const ts = timestamp > 10000000000000 ? timestamp / 1000000000 : timestamp;
    const seconds = dayjs().unix() - ts;
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
    };

    refresh();
    const interval = setInterval(refresh, 10000);

    // Listen to SSE for real-time events
    const eventSource = new EventSource('/events');
    const handleEvent = (e) => {
      try {
        const evt = JSON.parse(e.data);
        if (evt.icon && evt.text) {
          setRecentEvents(prev => [evt, ...prev].slice(0, 10));
        }
      } catch (err) {
        console.error('Error parsing SSE event:', err, e.data);
      }
    };

    eventSource.addEventListener('social', handleEvent);
    eventSource.addEventListener('ping', handleEvent);
    eventSource.addEventListener('observation', handleEvent);
    eventSource.addEventListener('hey-there', handleEvent);
    eventSource.addEventListener('chau', handleEvent);
    eventSource.addEventListener('seen', handleEvent);

    eventSource.onerror = (err) => {
      console.error('SSE error:', err);
    };

    return () => {
      clearInterval(interval);
      eventSource.close();
    };
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

  const auraDot = (naraName) => {
    const color = naraColors[naraName] || '#888';
    return (
      <span style={{
        display: 'inline-block',
        width: '8px',
        height: '8px',
        borderRadius: '50%',
        backgroundColor: color,
        marginRight: '6px',
        boxShadow: `0 0 3px ${color}`,
        verticalAlign: 'middle'
      }}></span>
    );
  };

  return (
    <div style={{ display: 'flex', gap: '20px', marginBottom: '20px' }}>
      <div style={{ flex: 1, padding: '10px', border: '1px solid #ddd', borderRadius: '8px' }}>
        <strong>💭 {server}'s Opinions:</strong>
        {sortedClout.length === 0 && <div style={{ color: '#888', marginTop: '8px' }}>No opinions yet...</div>}
        <div style={{ marginTop: '8px' }}>
          {sortedClout.map(([name, score]) => (
            <div key={name} style={{ display: 'flex', justifyContent: 'space-between', padding: '2px 0' }}>
              <span>{auraDot(name)}{name}</span>
              <span>{cloutEmoji(score)} {score.toFixed(1)}</span>
            </div>
          ))}
        </div>
      </div>
      <div style={{ flex: 1, padding: '10px', border: '1px solid #ddd', borderRadius: '8px' }}>
        <strong>📡 Network Activity:</strong>
        {recentEvents.length === 0 && <div style={{ color: '#888', marginTop: '8px' }}>No activity yet...</div>}
        <div style={{ marginTop: '8px', maxHeight: '200px', overflowY: 'auto' }}>
          {recentEvents.map((event, i) => (
            <div key={i} style={{ padding: '4px 0', borderBottom: i < recentEvents.length - 1 ? '1px solid #eee' : 'none' }}>
              <div style={{ fontSize: '0.9em' }}>
                <span style={{ marginRight: '4px' }}>{event.icon}</span>
                <span>{event.text}</span>
                <span style={{ color: '#888', marginLeft: '8px', fontSize: '0.85em' }}>{timeAgo(event.timestamp)}</span>
              </div>
              {event.detail && (
                <div style={{ fontSize: '0.85em', color: '#666', marginLeft: '20px' }}>{event.detail}</div>
              )}
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

  useEffect(() => {
    var lastDate = 0;

    const refresh = () => {
      const fetchDate = new Date;
      const now = dayjs().unix();
      const yesterday = now - 24 * 3600;
      const tenMinutesAgo = now - 10 * 60;

      window.fetch("/narae.json")
        .then(response => response.json())
        .then(function(data) {
          if(fetchDate > lastDate) {
            const filteredNaras = data.naras
              .filter(nara => {
                // Must be seen in the last 24 hours
                if (nara.LastSeen <= yesterday) return false;

                // Don't show missing nara if they're less than a day old and been missing for more than 10 minutes
                const isNew = nara.StartTime > yesterday;
                const isMissing = nara.LastSeen < tenMinutesAgo;
                if (isNew && isMissing) return false;

                return true;
              })
              .sort((a, b) => {
                const aUnknown = !isFinite(a.StartTime) || a.StartTime <= 0;
                const bUnknown = !isFinite(b.StartTime) || b.StartTime <= 0;
                if (aUnknown && bUnknown) return a.Name.localeCompare(b.Name);
                if (aUnknown) return 1;
                if (bUnknown) return -1;
                if (a.StartTime !== b.StartTime) return a.StartTime - b.StartTime;
                return a.Name.localeCompare(b.Name);
              });
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
      <div style={{display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '20px'}}>
        <a href="/stash.html" style={{padding: '8px 16px', background: '#007bff', color: 'white', borderRadius: '4px', textDecoration: 'none', fontSize: '14px'}}>
          📦 Stash Manager
        </a>
      </div>
      <TrendSummary naras={data.filteredNaras} />
      <table id="naras">
        <thead>
          <tr>
            <th>Name</th>
            <th>Flair</th>
            <th>Buzz</th>
            <th>Trend</th>
            <th>Chat</th>
            <th>Last Seen</th>
            <th>Running For</th>
            <th>First Seen</th>
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
      <SocialPanel />
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
    return dayjs.unix(timestamp).format('HH:mm:ss');
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
    return dayjs.unix(timestamp).format('MMM D, HH:mm:ss');
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

// Network Map Panel using D3.js
function NetworkMapPanel() {
  const { useState, useEffect, useRef } = React;
  const [isExpanded, setIsExpanded] = useState(false);
  const [mapData, setMapData] = useState({ nodes: [], server: '' });
  const [tooltip, setTooltip] = useState(null);
  const svgRef = useRef(null);

  // Fetch network map data
  useEffect(() => {
    if (!isExpanded) return;

    const fetchMap = () => {
      window.fetch("/network/map")
        .then(response => response.json())
        .then(data => {
          setMapData(data);
        })
        .catch(() => {});
    };

    fetchMap();
    const interval = setInterval(fetchMap, 10000);
    return () => clearInterval(interval);
  }, [isExpanded]);

  // Render D3 visualization
  useEffect(() => {
    if (!isExpanded || !svgRef.current || mapData.nodes.length === 0) return;

    const svg = d3.select(svgRef.current);
    const width = 600;
    const height = 400;

    // Clear previous content
    svg.selectAll("*").remove();

    const visibleNodes = mapData.nodes.filter(n => n.online);
    // Separate nodes with and without coordinates
    const nodesWithCoords = visibleNodes.filter(n => n.coordinates && n.coordinates.x !== undefined);
    const nodesWithoutCoords = visibleNodes.filter(n => !n.coordinates || n.coordinates.x === undefined);

    // Calculate bounds of coordinates (only from nodes that have them)
    let minX = Infinity, maxX = -Infinity;
    let minY = Infinity, maxY = -Infinity;

    nodesWithCoords.forEach(node => {
      minX = Math.min(minX, node.coordinates.x);
      maxX = Math.max(maxX, node.coordinates.x);
      minY = Math.min(minY, node.coordinates.y);
      maxY = Math.max(maxY, node.coordinates.y);
    });

    // Handle edge cases (no coordinates or all same point)
    if (!isFinite(minX) || minX === maxX) {
      minX = -1; maxX = 1;
    }
    if (!isFinite(minY) || minY === maxY) {
      minY = -1; maxY = 1;
    }

    // Add padding
    const padding = 0.2;
    const rangeX = maxX - minX;
    const rangeY = maxY - minY;
    minX -= rangeX * padding;
    maxX += rangeX * padding;
    minY -= rangeY * padding;
    maxY += rangeY * padding;

    // Create scales
    const xScale = d3.scaleLinear()
      .domain([minX, maxX])
      .range([50, width - 50]);

    const yScale = d3.scaleLinear()
      .domain([minY, maxY])
      .range([height - 50, 50]);

    // Draw grid lines
    const gridLines = svg.append("g").attr("class", "grid");
    for (let i = 0; i <= 4; i++) {
      const x = 50 + (width - 100) * i / 4;
      const y = 50 + (height - 100) * i / 4;
      gridLines.append("line")
        .attr("x1", x).attr("y1", 50)
        .attr("x2", x).attr("y2", height - 50)
        .attr("stroke", "#2d2d4a").attr("stroke-width", 1);
      gridLines.append("line")
        .attr("x1", 50).attr("y1", y)
        .attr("x2", width - 50).attr("y2", y)
        .attr("stroke", "#2d2d4a").attr("stroke-width", 1);
    }

    // Draw nodes WITH coordinates (positioned by Vivaldi)
    const positionedNodes = svg.selectAll(".node-positioned")
      .data(nodesWithCoords)
      .enter()
      .append("g")
      .attr("class", "node")
      .attr("transform", d => {
        const x = xScale(d.coordinates.x);
        const y = yScale(d.coordinates.y);
        return `translate(${x}, ${y})`;
      });

    positionedNodes.append("circle")
      .attr("class", d => "node-circle" + (d.is_self ? " self" : ""))
      .attr("r", d => d.is_self ? 12 : 8)
      .attr("fill", d => {
        if (d.is_self) return "#ffd700";
        return d.online ? "#48bb78" : "#a0aec0";
      })
      .on("mouseenter", function(event, d) {
        const rect = svgRef.current.getBoundingClientRect();
        setTooltip({
          x: event.clientX - rect.left + 10,
          y: event.clientY - rect.top - 10,
          node: d
        });
      })
      .on("mouseleave", () => setTooltip(null));

    positionedNodes.append("text")
      .attr("class", "node-label")
      .attr("y", d => d.is_self ? 25 : 20)
      .text(d => d.name);

    // Draw nodes WITHOUT coordinates (stacked on the right side)
    if (nodesWithoutCoords.length > 0) {
      // Draw a separator area for unknown positions
      svg.append("rect")
        .attr("x", width - 45)
        .attr("y", 45)
        .attr("width", 40)
        .attr("height", height - 90)
        .attr("fill", "#2d2d4a")
        .attr("rx", 5);

      svg.append("text")
        .attr("x", width - 25)
        .attr("y", 60)
        .attr("fill", "#666")
        .attr("font-size", "8px")
        .attr("text-anchor", "middle")
        .text("?");

      const unknownNodes = svg.selectAll(".node-unknown")
        .data(nodesWithoutCoords)
        .enter()
        .append("g")
        .attr("class", "node")
        .attr("transform", (d, i) => {
          const x = width - 25;
          const y = 80 + i * 20;
          return `translate(${x}, ${y})`;
        });

      unknownNodes.append("circle")
        .attr("class", "node-circle")
        .attr("r", 6)
        .attr("fill", d => d.online ? "#48bb78" : "#a0aec0")
        .attr("opacity", 0.6)
        .on("mouseenter", function(event, d) {
          const rect = svgRef.current.getBoundingClientRect();
          setTooltip({
            x: event.clientX - rect.left + 10,
            y: event.clientY - rect.top - 10,
            node: d
          });
        })
        .on("mouseleave", () => setTooltip(null));

      unknownNodes.append("text")
        .attr("class", "node-label")
        .attr("x", -15)
        .attr("y", 3)
        .attr("text-anchor", "end")
        .attr("font-size", "8px")
        .text(d => d.name);
    }

  }, [isExpanded, mapData]);

  return (
    <div className="network-map-container">
      <div className="network-map-header">
        <h3>Network Map</h3>
        <button className="network-map-toggle" onClick={() => setIsExpanded(!isExpanded)}>
          {isExpanded ? 'Hide Map' : 'Show Map'}
        </button>
      </div>

      {isExpanded && (
        <React.Fragment>
          <div className="network-map" style={{ position: 'relative' }}>
            <svg ref={svgRef} width="600" height="400"></svg>
            {tooltip && (
              <div className="node-tooltip" style={{ left: tooltip.x, top: tooltip.y }}>
                <strong>{tooltip.node.name}</strong>
                {tooltip.node.is_self && " (you)"}
                <br />
                Status: Online
                {tooltip.node.rtt_to_us !== undefined && (
                  <React.Fragment>
                    <br />
                    RTT: {tooltip.node.rtt_to_us.toFixed(1)}ms
                  </React.Fragment>
                )}
                {tooltip.node.coordinates && tooltip.node.coordinates.x !== undefined ? (
                  <React.Fragment>
                    <br />
                    Position: ({tooltip.node.coordinates.x.toFixed(2)}, {tooltip.node.coordinates.y.toFixed(2)})
                    <br />
                    Confidence: {((1 - tooltip.node.coordinates.error) * 100).toFixed(0)}%
                  </React.Fragment>
                ) : (
                  <React.Fragment>
                    <br />
                    <span style={{ color: '#999' }}>Position unknown (older version)</span>
                  </React.Fragment>
                )}
              </div>
            )}
          </div>
          <div className="network-map-legend">
            <span><div className="legend-dot self"></div> You</span>
            <span><div className="legend-dot online"></div> Online</span>
            <span><div className="legend-dot online" style={{ opacity: 0.5 }}></div> Unknown position</span>
          </div>
        </React.Fragment>
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
      <NetworkMapPanel />
      <NaraList />
    </React.Fragment>
  );
}

const domContainer = document.querySelector('#naras_container');
ReactDOM.render(React.createElement(App), domContainer);
