// nara - Projections View (Uptime, Clout, Opinions)

import { useState, useEffect } from 'preact/hooks';
import { useLocation } from './router.jsx';
import { formatDuration, formatDateRange, dayjs } from './utils.js';

function UptimeTimeline({ subject, onClose }) {
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  useEffect(() => {
    const fetchUptime = async () => {
      setLoading(true);
      setError(null);
      try {
        const response = await fetch(`/api/inspector/uptime/${encodeURIComponent(subject)}`);
        if (!response.ok) throw new Error('Failed to fetch uptime data');
        const result = await response.json();
        setData(result);
      } catch (err) {
        setError(err.message);
      } finally {
        setLoading(false);
      }
    };

    fetchUptime();
  }, [subject]);

  if (loading) return <div className="loading-spinner">💫</div>;

  if (error) {
    return (
      <div className="empty-state">
        <div className="empty-state-icon">❌</div>
        <div className="empty-state-text">{error}</div>
      </div>
    );
  }

  if (!data?.periods?.length) {
    return (
      <div className="empty-state">
        <div className="empty-state-icon">📭</div>
        <div className="empty-state-text">No uptime data available</div>
        <div className="empty-state-hint">Status events will appear as they occur</div>
      </div>
    );
  }

  return (
    <div className="uptime-container">
      <div className="uptime-header">
        <div className="uptime-subject">
          {data.baseline_source && (
            <span className="uptime-baseline-badge">
              {data.baseline_source === 'checkpoint' ? <><i className="iconoir-camera"></i> checkpoint</> : <><i className="iconoir-page"></i> backfill</>}
            </span>
          )}
          <span className="uptime-avatar"><i className="iconoir-clock"></i></span>
          <span className="uptime-name">{data.subject}</span>
        </div>
        <div className="uptime-total">
          <span className="uptime-total-label">Total Uptime</span>
          <span className="uptime-total-value">{formatDuration(data.total_uptime)}</span>
        </div>
      </div>

      <div className="uptime-timeline">
        {data.periods.map((period, index) => (
          <div key={index} className={`uptime-period ${period.type} ${period.ongoing ? 'ongoing' : ''}`}>
            <div className="uptime-period-icon">
              {period.type === 'historical' ? <i className="iconoir-camera"></i> : period.type === 'online' ? (period.ongoing ? <i className="iconoir-check-circle"></i> : <i className="iconoir-check"></i>) : <i className="iconoir-xmark-circle"></i>}
            </div>
            <div className="uptime-period-content">
              <div className="uptime-period-title">
                {period.type === 'historical'
                  ? 'Historical uptime'
                  : period.type === 'online'
                    ? (period.ongoing ? 'Running since' : 'Ran for')
                    : 'Offline for'}
                <span className="uptime-period-duration">{formatDuration(period.duration)}</span>
              </div>
              <div className="uptime-period-dates">
                {period.type === 'historical'
                  ? `First seen: ${dayjs.unix(period.start_time).format('MMM D, YYYY')} → Checkpoint: ${dayjs.unix(period.end_time).format('MMM D, YYYY')}`
                  : formatDateRange(period.start_time, period.end_time, period.ongoing)}
              </div>
            </div>
            {period.ongoing && <div className="uptime-period-badge">LIVE</div>}
            {period.type === 'historical' && <div className="uptime-period-badge historical">CHECKPOINT</div>}
          </div>
        ))}
      </div>

      {onClose && <button className="uptime-close" onClick={onClose}>Close</button>}
    </div>
  );
}

export function ProjectionExplorer({ initialUptimeSubject, initialTab }) {
  const [, navigate] = useLocation();
  const [activeTab, setActiveTab] = useState(initialTab || 'uptime');
  const [projections, setProjections] = useState({ online_status: {}, clout: {}, opinions: {} });
  const [loading, setLoading] = useState(true);
  const [selectedProjection, setSelectedProjection] = useState(null);
  const [selectedUptimeSubject, setSelectedUptimeSubject] = useState(initialUptimeSubject || null);

  useEffect(() => {
    if (selectedUptimeSubject) {
      navigate(`/projections/uptime/${encodeURIComponent(selectedUptimeSubject)}`);
    }
  }, [selectedUptimeSubject]);

  useEffect(() => {
    const fetchProjections = async () => {
      try {
        const response = await fetch('/api/inspector/projections');
        const data = await response.json();
        setProjections({
          online_status: data.online_status || {},
          clout: data.clout || {},
          opinions: data.opinions || {}
        });
      } catch (err) {
        console.error('Failed to fetch projections:', err);
      } finally {
        setLoading(false);
      }
    };

    fetchProjections();
    const interval = setInterval(fetchProjections, 10000);
    return () => clearInterval(interval);
  }, []);

  const openProjectionDetail = async (type, subject) => {
    try {
      const response = await fetch(`/api/inspector/projection/${type}/${subject}`);
      const data = await response.json();
      setSelectedProjection(data);
    } catch (err) {
      console.error('Failed to fetch projection detail:', err);
    }
  };

  const clearUptimeSubject = () => {
    setSelectedUptimeSubject(null);
    navigate('/projections');
  };

  if (loading) return <div className="loading-spinner">💫</div>;

  return (
    <div className="projection-explorer">
      <div className="projection-tabs">
        <button
          className={`projection-tab ${activeTab === 'uptime' ? 'active' : ''}`}
          onClick={() => setActiveTab('uptime')}
        >
          ⏱️ Uptime Timeline
        </button>
        <button
          className={`projection-tab ${activeTab === 'clout' ? 'active' : ''}`}
          onClick={() => setActiveTab('clout')}
        >
          <i className="iconoir-star"></i> Clout Scores
        </button>
        <button
          className={`projection-tab ${activeTab === 'opinions' ? 'active' : ''}`}
          onClick={() => setActiveTab('opinions')}
        >
          💭 Opinion Consensus
        </button>
      </div>

      {activeTab === 'uptime' && (
        <div className="projection-card">
          {Object.keys(projections.online_status).length === 0 ? (
            <div className="empty-state">
              <div className="empty-state-icon">⏱️</div>
              <div className="empty-state-text">No naras to show</div>
              <div className="empty-state-hint">Uptime data will appear as naras come online</div>
            </div>
          ) : selectedUptimeSubject ? (
            <div>
              <button className="uptime-back-button" onClick={clearUptimeSubject}>
                ← Back to list
              </button>
              <UptimeTimeline subject={selectedUptimeSubject} onClose={clearUptimeSubject} />
            </div>
          ) : (
            <div>
              <div className="projection-hint">Select a nara to view their uptime timeline:</div>
              {Object.entries(projections.online_status).map(([name, status]) => (
                <div
                  key={name}
                  className="projection-item uptime-select-item"
                  onClick={() => setSelectedUptimeSubject(name)}
                >
                  <div className="projection-item-left">
                    <span className="uptime-select-icon">⏱️</span>
                    <span className="projection-name">{name}</span>
                    {status.total_uptime > 0 && (
                      <span className="uptime-total-badge">{formatDuration(status.total_uptime)}</span>
                    )}
                  </div>
                  <div className="uptime-select-right">
                    <div className="uptime-select-status">
                      <div className={`status-dot ${(status.status?.toLowerCase()) || 'missing'}`}></div>
                      <span>{status.status || 'MISSING'}</span>
                    </div>
                    <button
                      className="uptime-details-btn"
                      onClick={(e) => {
                        e.stopPropagation();
                        openProjectionDetail('online_status', name);
                      }}
                    >
                      Details
                    </button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {activeTab === 'clout' && (
        <div className="projection-card">
          {Object.keys(projections.clout).length === 0 ? (
            <div className="empty-state">
              <div className="empty-state-icon">😶</div>
              <div className="empty-state-text">No clout scores yet</div>
              <div className="empty-state-hint">Social interactions will build clout over time</div>
            </div>
          ) : (
            Object.entries(projections.clout)
              .sort((a, b) => b[1] - a[1])
              .map(([name, score]) => {
                const maxScore = Math.max(...Object.values(projections.clout));
                const percentage = maxScore > 0 ? (score / maxScore) * 100 : 0;

                return (
                  <div
                    key={name}
                    className="projection-item"
                    onClick={() => openProjectionDetail('clout', name)}
                  >
                    <span className="projection-name">{name}</span>
                    <div className="clout-bar-container">
                      <div className="clout-bar" style={{ width: `${percentage}%` }}>
                        {score > 0 && score.toFixed(1)}
                      </div>
                    </div>
                    <span className="clout-score">{score.toFixed(1)}</span>
                  </div>
                );
              })
          )}
        </div>
      )}

      {activeTab === 'opinions' && (
        <div className="projection-card">
          {Object.keys(projections.opinions).length === 0 ? (
            <div className="empty-state">
              <div className="empty-state-icon">🤔</div>
              <div className="empty-state-text">No opinion consensus yet</div>
              <div className="empty-state-hint">Observations will build consensus over time</div>
            </div>
          ) : (
            Object.entries(projections.opinions).map(([name, opinion]) => (
              <div
                key={name}
                className="projection-item"
                onClick={() => openProjectionDetail('opinion', name)}
              >
                <div className="projection-item-left">
                  <span className="projection-name">{name}</span>
                </div>
                <div className="opinion-details">
                  {opinion.restarts !== undefined && <div>Restarts: {opinion.restarts}</div>}
                  {opinion.observation_count !== undefined && <div>{opinion.observation_count} observations</div>}
                  {opinion.consensus && <div className="opinion-consensus">{opinion.consensus}</div>}
                </div>
              </div>
            ))
          )}
        </div>
      )}

      {selectedProjection && (
        <div className="modal-overlay" onClick={() => setSelectedProjection(null)}>
          <div className="modal-content" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <div className="modal-title">
                {selectedProjection.type === 'online_status' && <i className="iconoir-check-circle"></i>}
                {selectedProjection.type === 'clout' && <i className="iconoir-star"></i>}
                {selectedProjection.type === 'opinion' && <i className="iconoir-chat-bubble"></i>}
                {' '}{selectedProjection.subject}
              </div>
              <button className="modal-close" onClick={() => setSelectedProjection(null)}>×</button>
            </div>
            <div className="modal-body">
              <div className="detail-section">
                <div className="detail-label">DERIVED STATE</div>
                <pre className="json-viewer">{JSON.stringify(selectedProjection.derived_state, null, 2)}</pre>
              </div>

              {selectedProjection.source_events?.length > 0 && (
                <div className="detail-section">
                  <div className="detail-label">SOURCE EVENTS ({selectedProjection.source_events.length})</div>
                  <pre className="json-viewer">{JSON.stringify(selectedProjection.source_events, null, 2)}</pre>
                </div>
              )}

              {selectedProjection.consensus_method && (
                <div className="metadata-section">
                  <div className="detail-label">CONSENSUS METHOD</div>
                  <div>{selectedProjection.consensus_method}</div>
                  {selectedProjection.outliers_removed !== undefined && (
                    <div className="muted">Outliers removed: {selectedProjection.outliers_removed}</div>
                  )}
                </div>
              )}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
