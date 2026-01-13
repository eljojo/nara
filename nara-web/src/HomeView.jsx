// nara - Home View Components

import { useState, useEffect, useRef } from 'preact/hooks';
import { AvatarCanvas, clamp01 } from './nara-avatar.jsx';
import { Link, globalNavigate } from './router.jsx';
import { stringToColor, stringToHue, timeAgo, dayjs } from './utils.js';

function NaraCard({ nara, entryNumber }) {
  const pointerRef = useRef({ x: 0, y: 0, active: false });
  const isOnline = nara.Online === "ONLINE";
  const hue = stringToHue(nara.Name);

  const localTimeAgo = (a, addAgo = false) => {
    if (a <= 0 || !isFinite(a)) return "???";
    if (a < 60) {
      const rounded = Math.round(a / 5) * 5;
      return `${rounded}s${addAgo ? "" : ""}`;
    }
    const olderTime = dayjs().unix() - a;
    return dayjs().to(dayjs(olderTime * 1000), true);
  };

  let uptime = "OFFLINE";
  if (nara.Online === "ONLINE") {
    const uptimeSeconds = nara.LastSeen - nara.LastRestart;
    if (nara.LastRestart === 0 || uptimeSeconds > 31536000) {
      uptime = localTimeAgo(nara.LastSeen - nara.StartTime) + "?";
    } else {
      uptime = localTimeAgo(uptimeSeconds);
    }
  }

  const primaryColor = nara.Aura || `hsl(${hue}, 60%, 50%)`;
  const secondaryColor = nara.AuraSecondary || nara.Aura || `hsl(${hue}, 60%, 35%)`;
  const sociability = nara.Sociability || 50;
  const buzz = nara.Buzz || 0;
  const chill = nara.Chill || 50;
  const agreeableness = nara.Agreeableness || 50;

  // Stats for the bars (normalize to 0-100)
  const chattinessStat = Math.min(100, (nara.Chattiness || 50));
  const buzzStat = Math.min(100, Math.max(0, buzz * 10)); // buzz typically 0-10
  const chillStat = chill;
  const uptimeHours = nara.Uptime ? nara.Uptime / 3600 : 0;
  const uptimeStat = Math.min(100, uptimeHours / 24 * 100); // 24h = 100%

  const profilePath = `/nara/${encodeURIComponent(nara.Name)}`;
  const cardLink = (nara.Online === "ONLINE" && nara.PublicUrl)
    ? `${nara.PublicUrl.replace(/\/$/, '')}${profilePath}`
    : profilePath;
  const isExternal = nara.Online === "ONLINE" && nara.PublicUrl;

  const handleCardClick = (e) => {
    // Don't navigate if clicking on an interactive element
    if (e.target.closest('a, button')) return;
    if (isExternal) {
      window.open(cardLink, '_blank', 'noopener');
    } else {
      globalNavigate(cardLink);
    }
  };

  return (
    <div
      className={`narae-card ${isOnline ? 'online' : 'offline'}`}
      style={{ '--card-hue': hue, cursor: 'pointer' }}
      onClick={handleCardClick}
    >
      {/* Scan lines overlay */}
      <div className="narae-scanlines" />

      {/* Entry number */}
      <div className="narae-entry-number">
        <span className="entry-hash">#</span>
        <span className="entry-num">{String(entryNumber).padStart(3, '0')}</span>
      </div>

      {/* Status indicator */}
      <div className={`narae-status ${isOnline ? 'online' : 'offline'}`}>
        <span className="status-dot" />
        <span className="status-text">{isOnline ? 'ONLINE' : 'OFFLINE'}</span>
      </div>

      {/* Avatar section */}
      <div className="narae-avatar-section">
        <div className="narae-avatar-frame">
          {isOnline && nara.ID && AvatarCanvas ? (
            <div
              className="narae-avatar"
              onMouseEnter={() => { pointerRef.current.active = true; }}
              onMouseMove={(e) => {
                const rect = e.currentTarget.getBoundingClientRect();
                const cx = rect.left + rect.width / 2;
                const cy = rect.top + rect.height / 2;
                const nx = (e.clientX - cx) / (rect.width / 2);
                const ny = (e.clientY - cy) / (rect.height / 2);
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
                size={80}
                pointerRef={pointerRef}
              />
            </div>
          ) : (
            <div className="narae-avatar-placeholder">
              <span className="placeholder-icon">?</span>
            </div>
          )}
        </div>

        {/* Flair badge */}
        {(nara.LicensePlate || nara.Flair) && (
          <div className="narae-flair">
            {nara.LicensePlate}{nara.Flair}
          </div>
        )}
      </div>

      {/* Name and info */}
      <div className="narae-info">
        {isExternal ? (
          <a href={cardLink} target="_blank" rel="noopener" className="narae-name">
            {nara.Name}
          </a>
        ) : (
          <Link href={cardLink} className="narae-name">
            {nara.Name}
          </Link>
        )}

        {/* Type badges (trend) */}
        <div className="narae-types">
          {nara.Trend ? (
            <span className="narae-type" style={{ backgroundColor: stringToColor(nara.Trend) }}>
              {nara.TrendEmoji} {nara.Trend}
            </span>
          ) : (
            <span className="narae-type neutral">NO TYPE</span>
          )}
        </div>
      </div>

      {/* Stats section */}
      <div className="narae-stats">
        <div className="stat-row">
          <span className="stat-label">CHAT</span>
          <div className="stat-bar-container">
            <div className="stat-bar chat" style={{ width: `${chattinessStat}%` }} />
          </div>
          <span className="stat-value">{nara.Chattiness || '?'}</span>
        </div>
        <div className="stat-row">
          <span className="stat-label">BUZZ</span>
          <div className="stat-bar-container">
            <div className="stat-bar buzz" style={{ width: `${buzzStat}%` }} />
          </div>
          <span className="stat-value">{buzz}</span>
        </div>
        <div className="stat-row">
          <span className="stat-label">CHILL</span>
          <div className="stat-bar-container">
            <div className="stat-bar chill" style={{ width: `${chillStat}%` }} />
          </div>
          <span className="stat-value">{chill}</span>
        </div>
        <div className="stat-row">
          <span className="stat-label">UPTIME</span>
          <div className="stat-bar-container">
            <div className="stat-bar uptime" style={{ width: `${uptimeStat}%` }} />
          </div>
          <span className="stat-value">{uptime}</span>
        </div>
      </div>

      {/* Footer stats */}
      <div className="narae-footer">
        <div className="footer-stat">
          <span className="footer-label">SEEN</span>
          <span className="footer-value">
            {(nara.LastSeen < 5 && isOnline) ? "NOW" : localTimeAgo(dayjs().unix() - nara.LastSeen)}
          </span>
        </div>
        <div className="footer-stat">
          <span className="footer-label">RST</span>
          <span className="footer-value">{nara.Restarts || 0}</span>
        </div>
        <div className="footer-stat">
          <span className="footer-label">AGE</span>
          <span className="footer-value">{localTimeAgo(nara.LastSeen - nara.StartTime)}</span>
        </div>
      </div>
    </div>
  );
}

function NaraTable({ naras }) {
  const now = dayjs().unix();
  const tenMinutesAgo = now - 10 * 60;

  const formatUptime = (nara) => {
    if (nara.Online !== "ONLINE") return "offline";

    const uptimeSeconds = nara.LastSeen - nara.LastRestart;
    if (nara.LastRestart === 0 || uptimeSeconds > 31536000) {
      const totalUptime = nara.LastSeen - nara.StartTime;
      if (totalUptime <= 0 || !isFinite(totalUptime)) return "???";
      return timeAgo(nara.LastSeen - totalUptime);
    } else {
      if (uptimeSeconds <= 0 || !isFinite(uptimeSeconds)) return "???";
      return timeAgo(nara.LastSeen - uptimeSeconds);
    }
  };

  return (
    <div className="narae-table-container">
      <table className="narae-table">
        <thead>
          <tr>
            <th>#</th>
            <th>Name</th>
            <th>Status</th>
            <th>Trend</th>
            <th>Clout</th>
            <th>Chattiness</th>
            <th>Uptime</th>
            <th>Last Seen</th>
          </tr>
        </thead>
        <tbody>
          {naras.map((nara, index) => {
            const isOnline = nara.Online === "ONLINE";
            const isMissing = nara.LastSeen < tenMinutesAgo && isOnline;
            const statusClass = isMissing ? 'missing' : (isOnline ? 'online' : 'offline');
            const statusText = isMissing ? 'MISSING' : nara.Online;
            const hue = stringToColor(nara.Name + (nara.Soul || ''), true);

            const cardLink = `/nara/${nara.Name}`;

            // Handle clout - might be undefined or NaN
            const cloutDisplay = (nara.Clout !== undefined && isFinite(nara.Clout))
              ? Math.round(nara.Clout)
              : '?';

            // Handle chattiness - might be undefined
            const chattinessDisplay = (nara.Chattiness !== undefined && nara.Chattiness !== null)
              ? `${nara.Chattiness}%`
              : '?';

            return (
              <tr key={nara.Name} className={`narae-table-row ${statusClass}`}>
                <td className="narae-table-entry">{index + 1}</td>
                <td className="narae-table-name">
                  <Link href={cardLink} className="narae-table-link" style={{ '--row-hue': hue }}>
                    {nara.Name}
                  </Link>
                </td>
                <td className={`narae-table-status ${statusClass}`}>
                  <span className="status-dot"></span>
                  {statusText}
                </td>
                <td className="narae-table-trend">
                  {nara.Trend ? (
                    <span className="trend-badge" style={{ backgroundColor: stringToColor(nara.Trend) }}>
                      {nara.Trend}
                    </span>
                  ) : (
                    <span className="trend-badge neutral">NO TYPE</span>
                  )}
                </td>
                <td className="narae-table-stat">{cloutDisplay}</td>
                <td className="narae-table-stat">{chattinessDisplay}</td>
                <td className="narae-table-time">{formatUptime(nara)}</td>
                <td className="narae-table-time">
                  {nara.LastSeen > 0 ? timeAgo(nara.LastSeen) : 'never'}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function NaraList() {
  const [data, setData] = useState({ filteredNaras: [], server: 'unknown' });
  const [viewMode, setViewMode] = useState(() => {
    // Load saved view mode from localStorage
    const saved = localStorage.getItem('narae-view-mode');
    return saved || 'cards';
  });

  // Save view mode to localStorage when it changes
  useEffect(() => {
    localStorage.setItem('narae-view-mode', viewMode);
  }, [viewMode]);

  useEffect(() => {
    let lastDate = 0;

    const refresh = () => {
      const fetchDate = new Date();
      const now = dayjs().unix();
      const yesterday = now - 24 * 3600;
      const tenMinutesAgo = now - 10 * 60;

      fetch("/narae.json")
        .then(res => res.json())
        .then(result => {
          if (fetchDate > lastDate) {
            const filteredNaras = result.naras
              .filter(nara => {
                if (nara.LastSeen <= yesterday) return false;
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
            setData({ ...result, filteredNaras });
            lastDate = fetchDate;
          }
        })
        .catch(() => {});
    };

    refresh();
    const interval = setInterval(refresh, 2000);
    return () => clearInterval(interval);
  }, []);

  const onlineCount = data.filteredNaras.filter(n => n.Online === "ONLINE").length;
  const totalCount = data.filteredNaras.length;

  return (
    <div className="narae-container">
      {/* Header */}
      <div className="narae-header">
        <div className="narae-title">
          <span className="narae-icon">◈</span>
          <span className="narae-text">the narae</span>
        </div>
        <div className="narae-header-controls">
          <div className="view-toggle">
            <button
              className={`view-toggle-btn ${viewMode === 'cards' ? 'active' : ''}`}
              onClick={() => setViewMode('cards')}
              title="Card view"
            >
              ⊞
            </button>
            <button
              className={`view-toggle-btn ${viewMode === 'table' ? 'active' : ''}`}
              onClick={() => setViewMode('table')}
              title="Table view"
            >
              ☰
            </button>
          </div>
          <div className="narae-counter">
            <span className="counter-online">{onlineCount}</span>
            <span className="counter-sep">/</span>
            <span className="counter-total">{totalCount}</span>
            <span className="counter-label">REGISTERED</span>
          </div>
        </div>
      </div>

      {/* Card grid or Table view */}
      {viewMode === 'cards' ? (
        <div className="narae-grid">
          {data.filteredNaras.map((nara, index) => (
            <NaraCard key={nara.Name} nara={nara} entryNumber={index + 1} />
          ))}
        </div>
      ) : (
        <NaraTable naras={data.filteredNaras} />
      )}

      <div className="narae-rendered-by">
        <span className="rendered-icon">⚡</span>
        rendered by {data.server}
      </div>
    </div>
  );
}

export function HomeView() {
  return (
    <div className="home-view">
      <NaraList />
    </div>
  );
}
