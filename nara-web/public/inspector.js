'use strict';
dayjs.extend(window.dayjs_plugin_relativeTime);

// ============================================================================
// Utility Functions
// ============================================================================

function timeAgo(timestamp) {
  if (!timestamp || timestamp === 0) return 'never';

  // Handle both seconds and nanoseconds
  const ts = timestamp > 10000000000000 ? timestamp / 1000000000 : timestamp;
  const seconds = dayjs().unix() - ts;

  if (seconds < 60) return `${Math.round(seconds)}s ago`;
  if (seconds < 3600) return `${Math.round(seconds / 60)}m ago`;
  if (seconds < 86400) return `${Math.round(seconds / 3600)}h ago`;
  return `${Math.round(seconds / 86400)}d ago`;
}

function formatTimestamp(timestamp) {
  if (!timestamp || timestamp === 0) return 'never';
  const ts = timestamp > 10000000000000 ? timestamp / 1000000000 : timestamp;
  return dayjs.unix(ts).format('MMM D, YYYY HH:mm:ss');
}

// ============================================================================
// Event Detail Modal
// ============================================================================

function EventDetailModal({ event, onClose }) {
  const { useEffect } = React;

  // Close on escape key
  useEffect(() => {
    const handleEscape = (e) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', handleEscape);
    return () => document.removeEventListener('keydown', handleEscape);
  }, [onClose]);

  if (!event) return null;

  const verification = event.verification || {};
  const isSigned = verification.is_signed || false;
  const isValid = verification.signature_valid || false;

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal-content" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <div className="modal-title">🔎 Event Details</div>
          <button className="modal-close" onClick={onClose}>×</button>
        </div>
        <div className="modal-body">
          {/* Event Info */}
          <div style={{ marginBottom: '20px' }}>
            <div style={{ fontSize: '12px', color: '#999', marginBottom: '4px' }}>EVENT ID</div>
            <div style={{ fontFamily: 'monospace', fontSize: '13px', wordBreak: 'break-all' }}>
              {(event.event && event.event.id) || 'unknown'}
            </div>
          </div>

          <div style={{ marginBottom: '20px' }}>
            <div style={{ fontSize: '12px', color: '#999', marginBottom: '4px' }}>TIMESTAMP</div>
            <div style={{ fontSize: '14px' }}>
              {formatTimestamp(event.event && event.event.ts)} ({timeAgo(event.event && event.event.ts)})
            </div>
          </div>

          {/* Signature Verification */}
          {isSigned && (
            <div className={`verification-section ${isValid ? '' : 'failed'}`}>
              <div style={{ fontSize: '14px', fontWeight: '600', marginBottom: '12px' }}>
                {isValid ? '✓ Signature Verified' : '✗ Signature Verification Failed'}
              </div>

              <div className="verification-row">
                <span className="verification-label">Is Signed:</span>
                <span className={`verification-value ${isSigned ? 'success' : 'failure'}`}>
                  {isSigned ? 'Yes' : 'No'}
                </span>
              </div>

              <div className="verification-row">
                <span className="verification-label">Signature Valid:</span>
                <span className={`verification-value ${isValid ? 'success' : 'failure'}`}>
                  {isValid ? 'Yes' : 'No'}
                </span>
              </div>

              <div className="verification-row">
                <span className="verification-label">Public Key Known:</span>
                <span className={`verification-value ${verification.public_key_known ? 'success' : 'failure'}`}>
                  {verification.public_key_known ? 'Yes' : 'No'}
                </span>
              </div>

              {verification.verification_error && (
                <div style={{ marginTop: '12px', padding: '8px', background: 'rgba(255,107,107,0.1)', borderRadius: '6px' }}>
                  <div style={{ fontSize: '11px', color: '#999', marginBottom: '4px' }}>ERROR</div>
                  <div style={{ fontSize: '13px', color: '#ff6b6b' }}>{verification.verification_error}</div>
                </div>
              )}
            </div>
          )}

          {/* Full Event JSON */}
          <div style={{ marginTop: '20px' }}>
            <div style={{ fontSize: '12px', color: '#999', marginBottom: '8px' }}>FULL EVENT DATA</div>
            <div className="json-viewer">
              {JSON.stringify(event.event, null, 2)}
            </div>
          </div>

          {/* Metadata */}
          {event.metadata && (
            <div style={{ marginTop: '20px', padding: '12px', background: '#f8f9fa', borderRadius: '8px' }}>
              <div style={{ fontSize: '12px', color: '#999', marginBottom: '8px' }}>METADATA</div>
              {event.metadata.event_index !== undefined && (
                <div style={{ fontSize: '13px', marginBottom: '4px' }}>
                  Position: {event.metadata.event_index} of {event.metadata.total_events}
                </div>
              )}
              {event.metadata.age_seconds !== undefined && (
                <div style={{ fontSize: '13px' }}>
                  Age: {timeAgo(dayjs().unix() - event.metadata.age_seconds)}
                </div>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

// ============================================================================
// Timeline View
// ============================================================================

function TimelineView() {
  const { useState, useEffect, useRef } = React;
  const [events, setEvents] = useState([]);
  const [loading, setLoading] = useState(true);
  const [filters, setFilters] = useState({
    service: null,
    subject: ''
  });
  const [selectedEvent, setSelectedEvent] = useState(null);
  const eventSourceRef = useRef(null);

  const serviceTypes = ['social', 'ping', 'observation', 'checkpoint', 'hey-there', 'chau'];

  // Fetch initial events
  useEffect(() => {
    fetchEvents();
  }, [filters]);

  // Set up SSE for live updates
  useEffect(() => {
    eventSourceRef.current = new EventSource('/events');

    const handleNewEvent = (e) => {
      try {
        const data = JSON.parse(e.data);
        // Transform SSE format to match API format
        const event = {
          id: data.id,
          service: data.service,
          timestamp: data.timestamp,
          emitter: data.emitter,
          ui_format: {
            icon: data.icon,
            text: data.text,
            detail: data.detail
          }
        };
        // Add to events list if it matches filters
        if (!filters.service || event.service === filters.service) {
          setEvents(prev => [event, ...prev].slice(0, 100)); // Keep last 100
        }
      } catch (err) {
        console.error('Error parsing SSE event:', err);
      }
    };

    serviceTypes.forEach(service => {
      eventSourceRef.current.addEventListener(service, handleNewEvent);
    });

    return () => {
      if (eventSourceRef.current) {
        eventSourceRef.current.close();
      }
    };
  }, [filters]);

  const fetchEvents = async () => {
    setLoading(true);
    try {
      const params = new URLSearchParams();
      if (filters.service) params.append('service', filters.service);
      if (filters.subject) params.append('subject', filters.subject);
      params.append('limit', '50');

      const response = await fetch(`/api/inspector/events?${params}`);
      const data = await response.json();
      setEvents(data.events || []);
    } catch (err) {
      console.error('Failed to fetch events:', err);
    } finally {
      setLoading(false);
    }
  };

  const toggleServiceFilter = (service) => {
    setFilters(prev => ({
      ...prev,
      service: prev.service === service ? null : service
    }));
  };

  const openEventDetail = async (eventId) => {
    if (!eventId) {
      console.error('Event ID is undefined or empty');
      return;
    }

    try {
      const response = await fetch(`/api/inspector/event/${encodeURIComponent(eventId)}`);
      if (!response.ok) {
        console.error(`Failed to fetch event: ${response.status} ${response.statusText}`);
        return;
      }
      const data = await response.json();
      setSelectedEvent(data);
    } catch (err) {
      console.error('Failed to fetch event detail:', err);
    }
  };

  return (
    <div className="timeline-view">
      {/* Filter Bar */}
      <div className="filter-bar">
        <div className="filter-section">
          <label className="filter-label">Service Type</label>
          <div className="filter-pills">
            {serviceTypes.map(service => (
              <button
                key={service}
                className={`filter-pill ${filters.service === service ? 'active' : ''}`}
                onClick={() => toggleServiceFilter(service)}
              >
                {service}
              </button>
            ))}
          </div>
        </div>

        <div className="filter-section">
          <label className="filter-label">Subject</label>
          <input
            type="text"
            className="filter-input"
            placeholder="Filter by nara name..."
            value={filters.subject}
            onChange={(e) => setFilters(prev => ({ ...prev, subject: e.target.value }))}
          />
        </div>
      </div>

      {/* Event List */}
      {loading ? (
        <div className="loading-spinner">💫</div>
      ) : events.length === 0 ? (
        <div className="empty-state">
          <div className="empty-state-icon">📭</div>
          <div className="empty-state-text">No events found</div>
          <div className="empty-state-hint">Try adjusting your filters</div>
        </div>
      ) : (
        <div className="event-list">
          {events.map((event, index) => (
            <div
              key={event.id || index}
              className={`event-card ${index === 0 ? 'new' : ''} ${!event.id ? 'no-detail' : ''}`}
              onClick={() => event.id && openEventDetail(event.id)}
              style={{ cursor: event.id ? 'pointer' : 'default' }}
            >
              <div className="event-header">
                <div className="event-icon">{(event.ui_format && event.ui_format.icon) || '📄'}</div>
                <div className="event-text">{(event.ui_format && event.ui_format.text) || event.service}</div>
                <div className="event-time">{timeAgo(event.timestamp)}</div>
              </div>
              {event.ui_format && event.ui_format.detail && (
                <div className="event-detail">{event.ui_format.detail}</div>
              )}
              {event.signed && (
                <div className="event-signed-badge">
                  ✓ Signed
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      {/* Event Detail Modal */}
      {selectedEvent && (
        <EventDetailModal
          event={selectedEvent}
          onClose={() => setSelectedEvent(null)}
        />
      )}
    </div>
  );
}

// ============================================================================
// Checkpoint Inspector
// ============================================================================

function CheckpointInspector() {
  const { useState, useEffect } = React;
  const [checkpoints, setCheckpoints] = useState([]);
  const [loading, setLoading] = useState(true);
  const [selectedCheckpoint, setSelectedCheckpoint] = useState(null);

  useEffect(() => {
    fetchCheckpoints();
    const interval = setInterval(fetchCheckpoints, 10000);
    return () => clearInterval(interval);
  }, []);

  const fetchCheckpoints = async () => {
    try {
      const response = await fetch('/api/inspector/checkpoints');
      const data = await response.json();
      setCheckpoints(data.checkpoints || []);
    } catch (err) {
      console.error('Failed to fetch checkpoints:', err);
    } finally {
      setLoading(false);
    }
  };

  const openCheckpointDetail = async (subject) => {
    try {
      const response = await fetch(`/api/inspector/checkpoint/${subject}`);
      const data = await response.json();
      setSelectedCheckpoint(data);
    } catch (err) {
      console.error('Failed to fetch checkpoint detail:', err);
    }
  };

  return (
    <div className="checkpoint-inspector">
      {loading ? (
        <div className="loading-spinner">💫</div>
      ) : checkpoints.length === 0 ? (
        <div className="empty-state">
          <div className="empty-state-icon">📸</div>
          <div className="empty-state-text">No checkpoints yet</div>
          <div className="empty-state-hint">Checkpoints will appear here as naras reach consensus</div>
        </div>
      ) : (
        <div className="checkpoint-grid">
          {checkpoints.map(checkpoint => (
            <div
              key={checkpoint.subject}
              className="checkpoint-card"
              onClick={() => openCheckpointDetail(checkpoint.subject)}
            >
              <div className="checkpoint-header">
                <div className="checkpoint-avatar">📸</div>
                <div className="checkpoint-info">
                  <div className="checkpoint-subject">{checkpoint.subject}</div>
                  <div className="checkpoint-timestamp">
                    {formatTimestamp(checkpoint.as_of_time)}
                  </div>
                </div>
              </div>

              <div className="checkpoint-observation">
                <div className="observation-item">
                  <span className="observation-label">Restarts:</span>
                  <span className="observation-value">{checkpoint.restarts}</span>
                </div>
                <div className="observation-item">
                  <span className="observation-label">Total Uptime:</span>
                  <span className="observation-value">{Math.round(checkpoint.total_uptime / 3600)}h</span>
                </div>
                <div className="observation-item">
                  <span className="observation-label">Start Time:</span>
                  <span className="observation-value">{formatTimestamp(checkpoint.start_time)}</span>
                </div>
                <div className="observation-item">
                  <span className="observation-label">Round:</span>
                  <span className="observation-value">{checkpoint.round}</span>
                </div>
              </div>

              <div style={{ fontSize: '13px', color: '#666', marginTop: '8px' }}>
                {checkpoint.verified_count} of {checkpoint.voter_count} signatures verified
                {checkpoint.all_verified && ' ✓'}
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Checkpoint Detail Modal */}
      {selectedCheckpoint && (
        <div className="modal-overlay" onClick={() => setSelectedCheckpoint(null)}>
          <div className="modal-content" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <div className="modal-title">📸 Checkpoint: {selectedCheckpoint.checkpoint && selectedCheckpoint.checkpoint.subject}</div>
              <button className="modal-close" onClick={() => setSelectedCheckpoint(null)}>×</button>
            </div>
            <div className="modal-body">
              {/* Checkpoint Summary */}
              <div style={{ marginBottom: '20px' }}>
                <div style={{ fontSize: '12px', color: '#999', marginBottom: '8px' }}>SUMMARY</div>
                <div style={{ fontSize: '14px', marginBottom: '4px' }}>
                  Total Voters: {selectedCheckpoint.summary && selectedCheckpoint.summary.total_voters}
                </div>
                <div style={{ fontSize: '14px', marginBottom: '4px' }}>
                  Verified: {selectedCheckpoint.summary && selectedCheckpoint.summary.verified_voters}
                </div>
                <div style={{ fontSize: '14px' }}>
                  Self-Attestation: {selectedCheckpoint.summary && selectedCheckpoint.summary.is_self_attestation ? 'Yes' : 'No'}
                </div>
              </div>

              {/* Voter List */}
              <div style={{ marginBottom: '20px' }}>
                <div style={{ fontSize: '12px', color: '#999', marginBottom: '8px' }}>VOTERS</div>
                <div className="voter-list">
                  {selectedCheckpoint.voters && selectedCheckpoint.voters.map((voter, index) => (
                    <div
                      key={index}
                      className={`voter-badge ${voter.verified ? 'verified' : 'unverified'}`}
                    >
                      {voter.voter_name}
                    </div>
                  ))}
                </div>
              </div>

              {/* Full Checkpoint Payload */}
              <div>
                <div style={{ fontSize: '12px', color: '#999', marginBottom: '8px' }}>CHECKPOINT PAYLOAD</div>
                <div className="json-viewer">
                  {JSON.stringify(selectedCheckpoint.event && selectedCheckpoint.event.checkpoint, null, 2)}
                </div>
              </div>

              {/* Sync Event Metadata */}
              <div style={{ marginTop: '16px' }}>
                <div style={{ fontSize: '12px', color: '#999', marginBottom: '8px' }}>SYNC EVENT METADATA</div>
                <div className="json-viewer">
                  {JSON.stringify(selectedCheckpoint.event && {
                    id: selectedCheckpoint.event.id,
                    service: selectedCheckpoint.event.svc,
                    timestamp: selectedCheckpoint.event.ts
                  }, null, 2)}
                </div>
              </div>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

// ============================================================================
// Projection Explorer
// ============================================================================

// ============================================================================
// Uptime Timeline Component
// ============================================================================

function formatDuration(seconds) {
  if (seconds < 60) return `${seconds}s`;
  if (seconds < 3600) return `${Math.round(seconds / 60)}m`;
  if (seconds < 86400) {
    const hours = Math.floor(seconds / 3600);
    const mins = Math.round((seconds % 3600) / 60);
    return mins > 0 ? `${hours}h ${mins}m` : `${hours}h`;
  }
  const days = Math.floor(seconds / 86400);
  const hours = Math.round((seconds % 86400) / 3600);
  return hours > 0 ? `${days}d ${hours}h` : `${days}d`;
}

function formatDateRange(startTime, endTime, ongoing) {
  const start = dayjs.unix(startTime);
  const end = ongoing ? dayjs() : dayjs.unix(endTime);

  const startStr = start.format('MMM D, YYYY HH:mm');
  const endStr = ongoing ? 'now' : end.format('MMM D, YYYY HH:mm');

  return `${startStr} → ${endStr}`;
}

function UptimeTimeline({ subject, onClose }) {
  const { useState, useEffect } = React;
  const [data, setData] = useState(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState(null);

  useEffect(() => {
    fetchUptime();
  }, [subject]);

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

  if (loading) {
    return (
      <div className="uptime-container">
        <div className="loading-spinner">💫</div>
      </div>
    );
  }

  if (error) {
    return (
      <div className="uptime-container">
        <div className="empty-state">
          <div className="empty-state-icon">❌</div>
          <div className="empty-state-text">{error}</div>
        </div>
      </div>
    );
  }

  if (!data || !data.periods || data.periods.length === 0) {
    return (
      <div className="uptime-container">
        <div className="empty-state">
          <div className="empty-state-icon">📭</div>
          <div className="empty-state-text">No uptime data available</div>
          <div className="empty-state-hint">Status events will appear as they occur</div>
        </div>
      </div>
    );
  }

  return (
    <div className="uptime-container">
      {/* Summary Header */}
      <div className="uptime-header">
        <div className="uptime-subject">
          {data.baseline_source && (
            <span className="uptime-baseline-badge">
              {data.baseline_source === 'checkpoint' ? '📸 checkpoint' : '📋 backfill'}
            </span>
          )}
          <span className="uptime-avatar">⏱️</span>
          <span className="uptime-name">{data.subject}</span>
        </div>
        <div className="uptime-total">
          <span className="uptime-total-label">Total Uptime</span>
          <span className="uptime-total-value">{formatDuration(data.total_uptime)}</span>
        </div>
      </div>

      {/* Timeline */}
      <div className="uptime-timeline">
        {data.periods.map((period, index) => (
          <div
            key={index}
            className={`uptime-period ${period.type} ${period.ongoing ? 'ongoing' : ''}`}
          >
            <div className="uptime-period-icon">
              {period.type === 'historical' ? '📸' : period.type === 'online' ? (period.ongoing ? '🟢' : '✅') : '🔴'}
            </div>
            <div className="uptime-period-content">
              <div className="uptime-period-title">
                {period.type === 'historical'
                  ? 'Historical uptime'
                  : period.type === 'online'
                    ? (period.ongoing ? 'Running since' : 'Ran for')
                    : 'Offline for'
                }
                <span className="uptime-period-duration">
                  {formatDuration(period.duration)}
                </span>
              </div>
              <div className="uptime-period-dates">
                {period.type === 'historical'
                  ? `First seen: ${dayjs.unix(period.start_time).format('MMM D, YYYY')} → Checkpoint: ${dayjs.unix(period.end_time).format('MMM D, YYYY')}`
                  : formatDateRange(period.start_time, period.end_time, period.ongoing)
                }
              </div>
            </div>
            {period.ongoing && (
              <div className="uptime-period-badge">LIVE</div>
            )}
            {period.type === 'historical' && (
              <div className="uptime-period-badge historical">CHECKPOINT</div>
            )}
          </div>
        ))}
      </div>

      {onClose && (
        <button className="uptime-close" onClick={onClose}>
          Close
        </button>
      )}
    </div>
  );
}

function ProjectionExplorer() {
  const { useState, useEffect } = React;
  const [activeTab, setActiveTab] = useState('uptime');
  const [projections, setProjections] = useState({
    online_status: {},
    clout: {},
    opinions: {}
  });
  const [loading, setLoading] = useState(true);
  const [selectedProjection, setSelectedProjection] = useState(null);
  const [selectedUptimeSubject, setSelectedUptimeSubject] = useState(null);

  useEffect(() => {
    fetchProjections();
    const interval = setInterval(fetchProjections, 10000);
    return () => clearInterval(interval);
  }, []);

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

  const openProjectionDetail = async (type, subject) => {
    try {
      const response = await fetch(`/api/inspector/projection/${type}/${subject}`);
      const data = await response.json();
      setSelectedProjection(data);
    } catch (err) {
      console.error('Failed to fetch projection detail:', err);
    }
  };

  if (loading) {
    return <div className="loading-spinner">💫</div>;
  }

  return (
    <div className="projection-explorer">
      {/* Projection Type Tabs */}
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
          ✨ Clout Scores
        </button>
        <button
          className={`projection-tab ${activeTab === 'opinions' ? 'active' : ''}`}
          onClick={() => setActiveTab('opinions')}
        >
          💭 Opinion Consensus
        </button>
      </div>

      {/* Uptime Timeline View */}
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
              <button
                className="uptime-back-button"
                onClick={() => setSelectedUptimeSubject(null)}
              >
                ← Back to list
              </button>
              <UptimeTimeline
                subject={selectedUptimeSubject}
                onClose={() => setSelectedUptimeSubject(null)}
              />
            </div>
          ) : (
            <div>
              <div style={{ fontSize: '14px', color: '#666', marginBottom: '16px' }}>
                Select a nara to view their uptime timeline:
              </div>
              {Object.entries(projections.online_status).map(([name, status]) => (
                <div
                  key={name}
                  className="projection-item uptime-select-item"
                  onClick={() => setSelectedUptimeSubject(name)}
                >
                  <div className="projection-item-left">
                    <span className="uptime-select-icon">⏱️</span>
                    <span style={{ fontWeight: '500' }}>{name}</span>
                    {status.total_uptime > 0 && (
                      <span className="uptime-total-badge">{formatDuration(status.total_uptime)}</span>
                    )}
                  </div>
                  <div className="uptime-select-right">
                    <div className="uptime-select-status">
                      <div className={`status-dot ${(status.status && status.status.toLowerCase()) || 'missing'}`}></div>
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

      {/* Clout Scores View */}
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
                    <span style={{ fontWeight: '500', minWidth: '120px' }}>{name}</span>
                    <div className="clout-bar-container">
                      <div className="clout-bar" style={{ width: `${percentage}%` }}>
                        {score > 0 && score.toFixed(1)}
                      </div>
                    </div>
                    <span style={{ fontSize: '14px', fontWeight: '600', minWidth: '50px', textAlign: 'right' }}>
                      {score.toFixed(1)}
                    </span>
                  </div>
                );
              })
          )}
        </div>
      )}

      {/* Opinions View */}
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
                  <span style={{ fontWeight: '500' }}>{name}</span>
                </div>
                <div style={{ fontSize: '12px', color: '#666', textAlign: 'right' }}>
                  {opinion.restarts !== undefined && (
                    <div>Restarts: {opinion.restarts}</div>
                  )}
                  {opinion.observation_count !== undefined && (
                    <div>{opinion.observation_count} observations</div>
                  )}
                  {opinion.consensus && (
                    <div style={{ color: '#7bed9f', fontWeight: '600' }}>
                      {opinion.consensus}
                    </div>
                  )}
                </div>
              </div>
            ))
          )}
        </div>
      )}

      {/* Projection Detail Modal */}
      {selectedProjection && (
        <div className="modal-overlay" onClick={() => setSelectedProjection(null)}>
          <div className="modal-content" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <div className="modal-title">
                {selectedProjection.type === 'online_status' && '🟢'}
                {selectedProjection.type === 'clout' && '✨'}
                {selectedProjection.type === 'opinion' && '💭'}
                {' '}
                {selectedProjection.subject}
              </div>
              <button className="modal-close" onClick={() => setSelectedProjection(null)}>×</button>
            </div>
            <div className="modal-body">
              {/* Derived State */}
              <div style={{ marginBottom: '20px' }}>
                <div style={{ fontSize: '12px', color: '#999', marginBottom: '8px' }}>DERIVED STATE</div>
                <div className="json-viewer">
                  {JSON.stringify(selectedProjection.derived_state, null, 2)}
                </div>
              </div>

              {/* Source Events */}
              {selectedProjection.source_events && selectedProjection.source_events.length > 0 && (
                <div>
                  <div style={{ fontSize: '12px', color: '#999', marginBottom: '8px' }}>
                    SOURCE EVENTS ({selectedProjection.source_events.length})
                  </div>
                  <div className="json-viewer">
                    {JSON.stringify(selectedProjection.source_events, null, 2)}
                  </div>
                </div>
              )}

              {/* Consensus Method */}
              {selectedProjection.consensus_method && (
                <div style={{ marginTop: '20px', padding: '12px', background: '#f8f9fa', borderRadius: '8px' }}>
                  <div style={{ fontSize: '12px', color: '#999', marginBottom: '4px' }}>CONSENSUS METHOD</div>
                  <div style={{ fontSize: '14px' }}>{selectedProjection.consensus_method}</div>
                  {selectedProjection.outliers_removed !== undefined && (
                    <div style={{ fontSize: '13px', color: '#666', marginTop: '4px' }}>
                      Outliers removed: {selectedProjection.outliers_removed}
                    </div>
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

// ============================================================================
// Main Inspector App
// ============================================================================

function InspectorApp() {
  const { useState } = React;
  const [activeTab, setActiveTab] = useState('timeline');

  return (
    <div>
      {/* Tab Navigation */}
      <div className="tab-navigation">
        <button
          className={`tab-button ${activeTab === 'timeline' ? 'active' : ''}`}
          onClick={() => setActiveTab('timeline')}
        >
          📅 Timeline
        </button>
        <button
          className={`tab-button ${activeTab === 'checkpoints' ? 'active' : ''}`}
          onClick={() => setActiveTab('checkpoints')}
        >
          📸 Checkpoints
        </button>
        <button
          className={`tab-button ${activeTab === 'projections' ? 'active' : ''}`}
          onClick={() => setActiveTab('projections')}
        >
          🔮 Projections
        </button>
      </div>

      {/* Content Area */}
      <div className="inspector-content">
        {activeTab === 'timeline' && <TimelineView />}
        {activeTab === 'checkpoints' && <CheckpointInspector />}
        {activeTab === 'projections' && <ProjectionExplorer />}
      </div>
    </div>
  );
}

// ============================================================================
// Render App
// ============================================================================

const domContainer = document.querySelector('#inspector_root');
ReactDOM.render(React.createElement(InspectorApp), domContainer);
