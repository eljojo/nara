// nara - Postcards View (Around the World)

import { useState, useEffect } from 'preact/hooks';
import { stringToHue, seededRandom, dayjs } from './utils.js';

function JourneyReceipt({ journey }) {
  const [expanded, setExpanded] = useState(true);

  if (!journey.hops || journey.hops.length === 0) return null;

  const firstHop = journey.hops[0];
  const lastHop = journey.hops[journey.hops.length - 1];
  const journeyDuration = lastHop.timestamp - firstHop.timestamp;

  // Truncate signature for display
  const truncateSig = (sig) => {
    if (!sig) return '---';
    if (sig.length <= 16) return sig;
    return sig.slice(0, 8) + '...' + sig.slice(-8);
  };

  return (
    <div className="journey-receipt">
      <div className="receipt-tear-top" />

      <div className="receipt-body">
        <div className="receipt-header">
          <div className="receipt-logo">🧾</div>
          <div className="receipt-title">VERIFICATION RECEIPT</div>
          <div className="receipt-subtitle">nara network verification chain</div>
        </div>

        <div className="receipt-divider">- - - - - - - - - - - - - - -</div>

        <div className="receipt-meta">
          <div className="receipt-row">
            <span>MESSAGE:</span>
            <span className="receipt-value">"{journey.message}"</span>
          </div>
          <div className="receipt-row">
            <span>ORIGIN:</span>
            <span className="receipt-value">{journey.originator}</span>
          </div>
          <div className="receipt-row">
            <span>DATE:</span>
            <span className="receipt-value">{dayjs.unix(firstHop.timestamp).format('YYYY-MM-DD')}</span>
          </div>
          <div className="receipt-row">
            <span>STATUS:</span>
            <span className="receipt-value receipt-status">
              {journey.complete ? '✓ VERIFIED' : '⏳ IN TRANSIT'}
            </span>
          </div>
        </div>

        <div className="receipt-divider">================================</div>

        <div className="receipt-chain-header">
          <span>CHAIN OF CUSTODY</span>
          <button
            className="receipt-expand-btn"
            onClick={() => setExpanded(!expanded)}
          >
            [{expanded ? '-' : '+'}] {expanded ? 'HIDE' : 'SHOW'} SIGS
          </button>
        </div>

        <div className="receipt-chain">
          {journey.hops.map((hop, i) => (
            <div key={i} className="receipt-hop">
              <div className="receipt-hop-header">
                <span className="receipt-hop-num">#{String(i + 1).padStart(2, '0')}</span>
                <span className="receipt-hop-nara">{hop.nara}</span>
                <span className="receipt-hop-stamp">{hop.stamp || '✨'}</span>
              </div>
              <div className="receipt-hop-time">
                {dayjs.unix(hop.timestamp).format('HH:mm:ss')}
                {i > 0 && (
                  <span className="receipt-hop-delta">
                    (+{hop.timestamp - journey.hops[i - 1].timestamp}s)
                  </span>
                )}
              </div>
              {expanded && (
                <div className="receipt-signature">
                  <span className="receipt-sig-label">SIG:</span>
                  <span className="receipt-sig-value">{truncateSig(hop.signature)}</span>
                </div>
              )}
              {i < journey.hops.length - 1 && (
                <div className="receipt-hop-arrow">↓</div>
              )}
            </div>
          ))}
        </div>

        <div className="receipt-divider">- - - - - - - - - - - - - - -</div>

        <div className="receipt-totals">
          <div className="receipt-row">
            <span>TOTAL HOPS:</span>
            <span className="receipt-value">{journey.hops.length}</span>
          </div>
          <div className="receipt-row">
            <span>TOTAL TIME:</span>
            <span className="receipt-value">{journeyDuration}s</span>
          </div>
          {journey.rewards && Object.keys(journey.rewards).length > 0 && (
            <div className="receipt-row">
              <span>CLOUT EARNED:</span>
              <span className="receipt-value">
                {Object.values(journey.rewards).reduce((a, b) => a + b, 0)}
              </span>
            </div>
          )}
        </div>

        <div className="receipt-divider">================================</div>

        <div className="receipt-footer">
          <div className="receipt-barcode">
            {/* Simple barcode-like pattern based on journey hash */}
            {Array.from({ length: 30 }, (_, i) => {
              const char = (journey.message + journey.originator).charCodeAt(i % journey.message.length);
              return <span key={i} className={`bar ${char % 2 === 0 ? 'thin' : 'thick'}`} />;
            })}
          </div>
          <div className="receipt-thank-you">
            *** THANK YOU FOR TRAVELING ***
          </div>
          <div className="receipt-slogan">
            nara - a friendly network
          </div>
        </div>
      </div>

      <div className="receipt-tear-bottom" />
    </div>
  );
}

function PassportStamp({ hop, index }) {
  const hue = stringToHue(hop.nara);
  const seed = hop.nara.split('').reduce((a, c) => a + c.charCodeAt(0), 0);
  const rotation = (seededRandom(seed) - 0.5) * 20;
  const stampType = Math.floor(seededRandom(seed * 2) * 4);

  const shapes = ['circle', 'rounded', 'hexagon', 'rectangle'];
  const shape = shapes[stampType];

  const stampStyle = {
    transform: `rotate(${rotation}deg)`,
    '--stamp-hue': hue,
  };

  return (
    <div className={`passport-stamp stamp-${shape}`} style={stampStyle}>
      <div className="stamp-inner">
        <div className="stamp-emoji">{hop.stamp || '✨'}</div>
        <div className="stamp-name">{hop.nara}</div>
        <div className="stamp-time">{dayjs.unix(hop.timestamp).format('HH:mm')}</div>
      </div>
      <div className="stamp-border" />
    </div>
  );
}

function PassportPage({ journey }) {
  return (
    <div className={`passport-page ${journey.complete ? 'complete' : 'in-progress'}`}>
      <div className="passport-header">
        <div className="passport-title">
          <span className="passport-icon">✉️</span>
          <span className="passport-message">"{journey.message}"</span>
        </div>
        <div className="passport-meta">
          <span className="passport-origin">From: {journey.originator}</span>
          {journey.complete && (
            <span className="passport-complete-badge">
              ✓ Journey Complete
            </span>
          )}
        </div>
      </div>

      {/* Verification Receipt */}
      <JourneyReceipt journey={journey} />
    </div>
  );
}

export function PostcardsView() {
  const [message, setMessage] = useState('');
  const [journeys, setJourneys] = useState([]);
  const [sending, setSending] = useState(false);
  const [error, setError] = useState('');

  const fetchJourneys = () => {
    fetch("/world/journeys")
      .then(res => res.json())
      .then(data => {
        // Only update if data changed to prevent unnecessary re-renders
        const newJourneys = data.journeys || [];
        setJourneys(prev => {
          if (JSON.stringify(prev) === JSON.stringify(newJourneys)) return prev;
          return newJourneys;
        });
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

    fetch("/world/start", {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ message: message.trim() })
    })
      .then(res => {
        if (!res.ok) return res.text().then(text => { throw new Error(text); });
        return res.json();
      })
      .then(() => {
        setMessage('');
        fetchJourneys();
      })
      .catch(err => setError(err.message))
      .finally(() => setSending(false));
  };

  return (
    <div className="world-view">
      <div className="passport-book">
        <div className="passport-cover">
          <div className="passport-emblem"><i className="iconoir-globe"></i></div>
          <h2>Around the World</h2>
          <p>Messages traveling across the nara network</p>
        </div>

        <div className="send-postcard">
          <div className="postcard-header">
            <span className="postcard-icon"><i className="iconoir-mail"></i></span>
            <span>Send a postcard around the world</span>
          </div>
          <div className="postcard-form">
            <input
              type="text"
              value={message}
              onChange={(e) => setMessage(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && startJourney()}
              placeholder="Write your message..."
              disabled={sending}
              className="postcard-input"
            />
            <button
              onClick={startJourney}
              disabled={sending || !message.trim()}
              className="postcard-send"
            >
              {sending ? '📤' : '✈️'} Send
            </button>
          </div>
          {error && <div className="postcard-error">{error}</div>}
        </div>

        <div className="passport-pages">
          {journeys.length === 0 ? (
            <div className="no-journeys">
              <div className="no-journeys-icon"><i className="iconoir-mail-out"></i></div>
              <p>No completed journeys yet</p>
              <p className="hint">Send a postcard to start one!</p>
            </div>
          ) : (
            journeys.map((journey, idx) => (
              <PassportPage key={journey.id || idx} journey={journey} />
            ))
          )}
        </div>
      </div>
    </div>
  );
}
