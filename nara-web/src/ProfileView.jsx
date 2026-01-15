// nara - Profile View (Nara Details Page)

import { useState, useEffect, useRef } from 'preact/hooks';
import { AvatarCanvas, clamp01 } from './nara-avatar.jsx';
import { Link } from './router.jsx';
import { stringToHue, timeAgo } from './utils.js';

export function ProfileView({ name }) {
  const [profile, setProfile] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);
  const [checkpoint, setCheckpoint] = useState(null);
  const [checkpointModal, setCheckpointModal] = useState(null);
  const pointerRef = useRef({ x: 0, y: 0, active: false });

  useEffect(() => {
    setLoading(true);
    setError(null);
    fetch(`/profile/${encodeURIComponent(name)}.json`)
      .then(res => {
        if (!res.ok) throw new Error('Nara not found');
        return res.json();
      })
      .then(data => {
        setProfile(data);
        setLoading(false);
      })
      .catch(err => {
        setError(err.message);
        setLoading(false);
      });
  }, [name]);

  // Fetch checkpoint data for this nara
  useEffect(() => {
    fetch('/api/inspector/checkpoints')
      .then(res => res.json())
      .then(data => {
        const cp = (data.checkpoints || []).find(c => c.subject === name);
        setCheckpoint(cp || null);
      })
      .catch(() => setCheckpoint(null));
  }, [name]);

  const openCheckpointModal = async () => {
    try {
      const response = await fetch(`/api/inspector/checkpoint/${encodeURIComponent(name)}`);
      const data = await response.json();
      setCheckpointModal(data);
    } catch (err) {
      console.error('Failed to fetch checkpoint detail:', err);
    }
  };

  if (loading) {
    return (
      <div className="profile-view">
        <div className="profile-loading">
          <div className="loading-spinner" />
          <p>Finding {name}...</p>
        </div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="profile-view">
        <div className="profile-error">
          <div className="error-icon">🔍</div>
          <h2>Nara not found</h2>
          <p>Couldn't find a nara named "{name}"</p>
          <Link href="/" className="back-link">← Back to network</Link>
        </div>
      </div>
    );
  }

  const nara = profile;
  const displayName = nara.name || nara.Name || name;
  const isOnline = (nara.online || nara.Online) === 'ONLINE';
  const hue = stringToHue(displayName);

  // Extract data with fallbacks
  const personality = nara.personality || nara.Personality || {};
  const aura = nara.aura || nara.Aura || {};
  const hostStats = nara.host_stats || nara.HostStats || {};
  const observations = nara.observations || nara.Observations || {};

  const sociability = personality.Sociability ?? 50;
  const agreeableness = personality.Agreeableness ?? 50;
  const chill = personality.Chill ?? 50;
  const chattiness = nara.chattiness ?? 50;
  const buzz = nara.buzz ?? nara.Buzz ?? 0;

  const primaryColor = aura.primary || `hsl(${hue}, 60%, 50%)`;
  const secondaryColor = aura.secondary || primaryColor;

  const memoryBudgetMB = nara.memory_budget_mb || nara.MemoryBudgetMB || 0;
  const memoryMaxEvents = nara.memory_max_events || nara.MemoryMaxEvents || 0;
  const eventStoreTotal = nara.event_store_total ?? nara.EventStoreTotal ?? 0;
  const eventStoreByService = nara.event_store_by_service || nara.EventStoreByService || {};

  const lastSeen = nara.last_seen || nara.LastSeen || 0;
  const lastRestart = nara.last_restart || nara.LastRestart || 0;
  const restarts = nara.restarts ?? nara.Restarts ?? 0;
  const meshIP = nara.mesh_ip || nara.MeshIP || '';
  const transportMode = nara.transport_mode || nara.TransportMode || 'hybrid';

  const trend = nara.trend || nara.Trend || '';
  const trendEmoji = nara.trend_emoji || nara.TrendEmoji || '';
  const flair = nara.flair || nara.Flair || '';
  const licensePlate = nara.license_plate || nara.LicensePlate || '';

  // Calculate uptime
  const uptimeSec = lastSeen && lastRestart ? Math.max(0, lastSeen - lastRestart) : 0;
  const formatDuration = (sec) => {
    if (!sec) return '-';
    if (sec < 60) return `${sec}s`;
    if (sec < 3600) return `${Math.floor(sec / 60)}m`;
    if (sec < 86400) return `${Math.floor(sec / 3600)}h`;
    return `${Math.floor(sec / 86400)}d`;
  };

  // Recent teases
  const recentTeases = (nara.recent_teases || []).slice(0, 6);

  // Best friends
  const bestFriends = nara.best_friends || {};

  // Service breakdown for chart
  const serviceEntries = Object.entries(eventStoreByService)
    .sort((a, b) => b[1] - a[1])
    .slice(0, 5);
  const maxServiceCount = serviceEntries.reduce((m, [, c]) => Math.max(m, c), 1);

  // Neighbours from observations
  const neighbourList = Object.entries(observations)
    .map(([n, o]) => ({
      name: n,
      online: o.Online || 'UNKNOWN',
      lastSeen: o.LastSeen || 0
    }))
    .sort((a, b) => b.lastSeen - a.lastSeen)
    .slice(0, 8);

  return (
    <div className="profile-view">
      <div className="profile-header">
        <Link href="/" className="back-button">← Network</Link>
      </div>

      <div className="profile-card">
        {/* Left: Avatar */}
        <div className="profile-avatar-section">
          <div
            className="profile-avatar-wrap"
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
            {(nara.id || nara.ID || displayName) && (
              <AvatarCanvas
                id={nara.id || nara.ID || displayName}
                primary={primaryColor}
                secondary={secondaryColor}
                sociability={sociability}
                chill={chill}
                agreeableness={agreeableness}
                buzz={buzz}
                size={200}
                pointerRef={pointerRef}
              />
            )}
          </div>

          {/* Decorative elements */}
          <div className="profile-sticker top-left">⭐</div>
          <div className="profile-sticker top-right">🚀</div>

          {(licensePlate || flair) && (
            <div className="profile-flair-badge">
              {licensePlate}{flair}
            </div>
          )}
        </div>

        {/* Right: Info */}
        <div className="profile-info-section">
          <div className="profile-name-row">
            <h1 className="profile-name">{displayName}</h1>
            <span className={`profile-status ${isOnline ? 'online' : 'offline'}`}>
              <span className="status-dot" />
              {isOnline ? 'ONLINE' : 'OFFLINE'}
            </span>
          </div>

          {memoryBudgetMB > 0 && (
            <div className="profile-memory-badge">
              💾 {memoryBudgetMB} MB • {memoryMaxEvents} events
            </div>
          )}

          {trend && (
            <div className="profile-trend">
              {trendEmoji} {trend}
            </div>
          )}

          {/* Quick stats grid */}
          <div className="profile-stats-grid">
            <div className="profile-stat">
              <span className="stat-label">Uptime</span>
              <span className="stat-value">{isOnline ? formatDuration(uptimeSec) : '-'}</span>
            </div>
            <div className="profile-stat">
              <span className="stat-label">Restarts</span>
              <span className="stat-value">{restarts}</span>
            </div>
            <div className="profile-stat">
              <span className="stat-label">Events</span>
              <span className="stat-value">{eventStoreTotal}</span>
            </div>
            <div className="profile-stat">
              <span className="stat-label">Buzz</span>
              <span className="stat-value">{buzz}</span>
            </div>
          </div>

          {/* Personality bars */}
          <div className="profile-personality">
            <h3>Personality</h3>
            <div className="personality-bars">
              {[
                { label: 'Sociability', value: sociability },
                { label: 'Agreeableness', value: agreeableness },
                { label: 'Chill', value: chill },
                { label: 'Chattiness', value: chattiness }
              ].map(({ label, value }) => (
                <div key={label} className="personality-bar-row">
                  <span className="bar-label">{label}</span>
                  <div className="bar-track">
                    <div className="bar-fill" style={{ width: `${Math.min(100, value)}%` }} />
                  </div>
                  <span className="bar-value">{value}</span>
                </div>
              ))}
            </div>
          </div>
        </div>
      </div>

      {/* Additional sections */}
      <div className="profile-details-grid">

        {/* Checkpoint card */}
        {checkpoint && (
          <div
            className="profile-detail-card checkpoint-preview"
            onClick={openCheckpointModal}
            style={{ cursor: 'pointer' }}
          >
            <h3><i className="iconoir-camera"></i> Checkpoint {checkpoint.version && checkpoint.version > 1 && <span className="version-badge">v{checkpoint.version}</span>}</h3>
            <div className="checkpoint-observations">
              <div className="observation-item">
                <span className="observation-label">Restarts:</span>
                <span className="observation-value">{checkpoint.restarts}</span>
              </div>
              <div className="observation-item">
                <span className="observation-label">Total Uptime:</span>
                <span className="observation-value">{Math.round(checkpoint.total_uptime / 3600)}h</span>
              </div>
              <div className="observation-item">
                <span className="observation-label">Round:</span>
                <span className="observation-value">{checkpoint.round}</span>
              </div>
            </div>
            <div className="checkpoint-signatures">
              {checkpoint.verified_count} of {checkpoint.voter_count} signatures verified
              {checkpoint.all_verified && ' ✓'}
            </div>
            {checkpoint.previous_checkpoint_id && (
              <div className="checkpoint-chain-indicator">
                🔗 Chained to previous checkpoint
              </div>
            )}
            <div className="checkpoint-hint">Click to view details</div>
          </div>
        )}

        {/* Host stats */}
        <div className="profile-detail-card">
          <h3>🖥️ Host Stats</h3>
          <div className="detail-list">
            <div className="detail-row">
              <span>Goroutines</span>
              <span>{hostStats.NumGoroutines ?? '-'}</span>
            </div>
            <div className="detail-row">
              <span>CPU</span>
              <span>{(hostStats.ProcCPUPercent ?? 0).toFixed(1)}%</span>
            </div>
            <div className="detail-row">
              <span>Memory (alloc)</span>
              <span>{hostStats.MemAllocMB ?? 0} MB</span>
            </div>
            <div className="detail-row">
              <span>Memory (sys)</span>
              <span>{hostStats.MemSysMB ?? 0} MB</span>
            </div>
            {meshIP && (
              <div className="detail-row">
                <span>Mesh</span>
                <span>{meshIP} ({transportMode})</span>
              </div>
            )}
          </div>
        </div>

        {/* Event store by service */}
        <div className="profile-detail-card">
          <h3>📊 Events by Service</h3>
          {serviceEntries.length > 0 ? (
            <div className="service-bars">
              {serviceEntries.map(([service, count]) => (
                <div key={service} className="service-bar-row">
                  <span className="service-name">{service}</span>
                  <div className="service-bar-track">
                    <div
                      className="service-bar-fill"
                      style={{ width: `${(count / maxServiceCount) * 100}%` }}
                    />
                  </div>
                  <span className="service-count">{count}</span>
                </div>
              ))}
            </div>
          ) : (
            <p className="empty-state">No events yet</p>
          )}
        </div>

        {/* Recent teases */}
        <div className="profile-detail-card">
          <h3>💬 Recent Teases</h3>
          {recentTeases.length > 0 ? (
            <div className="tease-list">
              {recentTeases.map((tease, idx) => (
                <div key={idx} className="tease-item">
                  <span className="tease-content">
                    <Link href={`/nara/${encodeURIComponent(tease.actor)}`}>{tease.actor}</Link>
                    {' → '}
                    <Link href={`/nara/${encodeURIComponent(tease.target)}`}>{tease.target}</Link>
                    : {tease.reason}
                  </span>
                  <span className="tease-time">{timeAgo(tease.timestamp)}</span>
                </div>
              ))}
            </div>
          ) : (
            <p className="empty-state">No recent teases</p>
          )}
        </div>

        {/* Best friends */}
        <div className="profile-detail-card">
          <h3>💕 Best Friends</h3>
          {(bestFriends.from?.name || bestFriends.to?.name) ? (
            <div className="friends-list">
              {bestFriends.from?.name && (
                <div className="friend-item">
                  <span>Most vibes from</span>
                  <Link href={`/nara/${encodeURIComponent(bestFriends.from.name)}`}>
                    {bestFriends.from.name}
                  </Link>
                  <span className="friend-count">{bestFriends.from.count} moments</span>
                </div>
              )}
              {bestFriends.to?.name && (
                <div className="friend-item">
                  <span>Most hype sent to</span>
                  <Link href={`/nara/${encodeURIComponent(bestFriends.to.name)}`}>
                    {bestFriends.to.name}
                  </Link>
                  <span className="friend-count">{bestFriends.to.count} moments</span>
                </div>
              )}
            </div>
          ) : (
            <p className="empty-state">No close buddies yet</p>
          )}
        </div>

        {/* Neighbours */}
        <div className="profile-detail-card">
          <h3>🏘️ Neighbourhood</h3>
          {neighbourList.length > 0 ? (
            <div className="neighbour-list">
              {neighbourList.map(n => (
                <div key={n.name} className="neighbour-item">
                  <span className={`neighbour-status ${n.online.toLowerCase()}`} />
                  <Link href={`/nara/${encodeURIComponent(n.name)}`}>{n.name}</Link>
                  <span className="neighbour-seen">{timeAgo(n.lastSeen)}</span>
                </div>
              ))}
            </div>
          ) : (
            <p className="empty-state">No neighbours recorded</p>
          )}
        </div>

        {/* Inspector links */}
        <div className="profile-detail-card">
          <h3><i className="iconoir-search"></i> Inspect</h3>
          <div className="inspector-links">
            <Link href={`/projections/uptime/${encodeURIComponent(displayName)}`} className="inspector-link">
              <i className="iconoir-graph-up"></i> Uptime Timeline
            </Link>
            <Link href={`/timeline?subject=${encodeURIComponent(displayName)}`} className="inspector-link">
              <i className="iconoir-calendar"></i> Events
            </Link>
          </div>
        </div>
      </div>

      {/* Checkpoint Modal */}
      {checkpointModal && (
        <div className="modal-overlay" onClick={() => setCheckpointModal(null)}>
          <div className="modal-content" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <div className="modal-title">
                <i className="iconoir-camera"></i> Checkpoint: {checkpointModal.event?.checkpoint?.subject}
              </div>
              <button className="modal-close" onClick={() => setCheckpointModal(null)}>×</button>
            </div>
            <div className="modal-body">
              <div className="detail-section">
                <div className="detail-label">SUMMARY</div>
                <div>Version: {checkpointModal.event?.checkpoint?.version || 1}</div>
                <div>Total Voters: {checkpointModal.summary?.total_voters}</div>
                <div>Verified: {checkpointModal.summary?.verified_voters}</div>
                <div>Self-Attestation: {checkpointModal.summary?.is_self_attestation ? 'Yes' : 'No'}</div>
              </div>

              {checkpointModal.event?.checkpoint?.previous_checkpoint_id && (
                <div className="detail-section checkpoint-chain-section">
                  <div className="detail-label">CHAIN OF TRUST</div>
                  <div className="checkpoint-chain-info">
                    <div className="chain-item current">
                      <div className="chain-icon">📍</div>
                      <div className="chain-details">
                        <div className="chain-label">Current Checkpoint</div>
                        <div className="chain-id">{checkpointModal.event?.id?.substring(0, 12)}...</div>
                        <div className="chain-time">{new Date(checkpointModal.event?.checkpoint?.as_of_time * 1000).toLocaleString()}</div>
                      </div>
                    </div>
                    <div className="chain-arrow">↓</div>
                    <div className="chain-item previous">
                      <div className="chain-icon">🔗</div>
                      <div className="chain-details">
                        <div className="chain-label">Previous Checkpoint</div>
                        <div className="chain-id">{checkpointModal.event?.checkpoint?.previous_checkpoint_id?.substring(0, 12)}...</div>
                        <div className="chain-note">Verifiable chain link</div>
                      </div>
                    </div>
                  </div>
                </div>
              )}

              <div className="detail-section">
                <div className="detail-label">VOTERS</div>
                <div className="voter-list">
                  {checkpointModal.voters?.map((voter, index) => (
                    <span key={index} className={`voter-badge ${voter.verified ? 'verified' : 'unverified'}`}>
                      {voter.voter_name}
                    </span>
                  ))}
                </div>
              </div>

              <div className="detail-section">
                <div className="detail-label">CHECKPOINT PAYLOAD</div>
                <pre className="json-viewer">
                  {JSON.stringify(checkpointModal.event?.checkpoint, null, 2)}
                </pre>
              </div>

              <div className="detail-section">
                <div className="detail-label">SYNC EVENT METADATA</div>
                <pre className="json-viewer">
                  {JSON.stringify({
                    id: checkpointModal.event?.id,
                    service: checkpointModal.event?.svc,
                    timestamp: checkpointModal.event?.ts
                  }, null, 2)}
                </pre>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
