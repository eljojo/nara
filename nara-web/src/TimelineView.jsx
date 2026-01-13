// nara - Timeline View (Events)

import { useState, useEffect, useRef } from 'preact/hooks';
import { useLocation } from './router.jsx';
import { timeAgo, formatTimestamp, dayjs } from './utils.js';

function EventDetailModal({ event, onClose }) {
  useEffect(() => {
    const handleEscape = (e) => { if (e.key === 'Escape') onClose(); };
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
          <div className="detail-section">
            <div className="detail-label">EVENT ID</div>
            <div className="detail-value mono">{event.event?.id || 'unknown'}</div>
          </div>

          <div className="detail-section">
            <div className="detail-label">TIMESTAMP</div>
            <div className="detail-value">
              {formatTimestamp(event.event?.ts)} ({timeAgo(event.event?.ts)})
            </div>
          </div>

          {isSigned && (
            <div className={`verification-section ${isValid ? '' : 'failed'}`}>
              <div className="verification-title">
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
                <div className="verification-error">
                  <div className="detail-label">ERROR</div>
                  <div className="error-text">{verification.verification_error}</div>
                </div>
              )}
            </div>
          )}

          <div className="detail-section">
            <div className="detail-label">FULL EVENT DATA</div>
            <pre className="json-viewer">{JSON.stringify(event.event, null, 2)}</pre>
          </div>

          {event.metadata && (
            <div className="metadata-section">
              <div className="detail-label">METADATA</div>
              {event.metadata.event_index !== undefined && (
                <div>Position: {event.metadata.event_index} of {event.metadata.total_events}</div>
              )}
              {event.metadata.age_seconds !== undefined && (
                <div>Age: {timeAgo(dayjs().unix() - event.metadata.age_seconds)}</div>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

export function TimelineView() {
  const [, navigate] = useLocation();
  const [events, setEvents] = useState([]);
  const [loading, setLoading] = useState(true);
  const [filters, setFilters] = useState(() => {
    const params = new URLSearchParams(window.location.search);
    return {
      service: params.get('service') || null,
      subject: params.get('subject') || ''
    };
  });
  const [selectedEvent, setSelectedEvent] = useState(null);
  const eventSourceRef = useRef(null);

  const serviceTypes = ['social', 'ping', 'observation', 'checkpoint', 'hey-there', 'chau'];

  useEffect(() => {
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

    fetchEvents();
  }, [filters]);

  useEffect(() => {
    eventSourceRef.current = new EventSource('/events');

    const handleNewEvent = (e) => {
      try {
        const data = JSON.parse(e.data);
        const event = {
          id: data.id,
          service: data.service,
          timestamp: data.timestamp,
          emitter: data.emitter,
          ui_format: { icon: data.icon, text: data.text, detail: data.detail }
        };
        if (!filters.service || event.service === filters.service) {
          setEvents(prev => [event, ...prev].slice(0, 100));
        }
      } catch (err) {
        console.error('Error parsing SSE event:', err);
      }
    };

    serviceTypes.forEach(service => {
      eventSourceRef.current.addEventListener(service, handleNewEvent);
    });

    return () => {
      if (eventSourceRef.current) eventSourceRef.current.close();
    };
  }, [filters]);

  const openEventDetail = async (eventId) => {
    if (!eventId) return;
    try {
      const response = await fetch(`/api/inspector/event/${encodeURIComponent(eventId)}`);
      if (response.ok) {
        const data = await response.json();
        setSelectedEvent(data);
        navigate(`/events/${encodeURIComponent(eventId)}`);
      }
    } catch (err) {
      console.error('Failed to fetch event detail:', err);
    }
  };

  const closeEventDetail = () => {
    setSelectedEvent(null);
    navigate('/timeline');
  };

  return (
    <div className="timeline-view">
      <div className="filter-bar">
        <div className="filter-section">
          <label className="filter-label">Service Type</label>
          <div className="filter-pills">
            {serviceTypes.map(service => (
              <button
                key={service}
                className={`filter-pill ${filters.service === service ? 'active' : ''}`}
                onClick={() => setFilters(prev => ({
                  ...prev,
                  service: prev.service === service ? null : service
                }))}
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

      {loading ? (
        <div className="loading-spinner"><i className="iconoir-refresh-double"></i></div>
      ) : events.length === 0 ? (
        <div className="empty-state">
          <div className="empty-state-icon"><i className="iconoir-mail-out"></i></div>
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
                <div className="event-icon">{event.ui_format?.icon || '📄'}</div>
                <div className="event-text">{event.ui_format?.text || event.service}</div>
                <div className="event-time">{timeAgo(event.timestamp)}</div>
              </div>
              {event.ui_format?.detail && (
                <div className="event-detail">{event.ui_format.detail}</div>
              )}
              {event.signed && <div className="event-signed-badge">✓ Signed</div>}
            </div>
          ))}
        </div>
      )}

      {selectedEvent && <EventDetailModal event={selectedEvent} onClose={closeEventDetail} />}
    </div>
  );
}

export function EventDetailPage({ eventId }) {
  const [, navigate] = useLocation();
  const [event, setEvent] = useState(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    const fetchEvent = async () => {
      try {
        const response = await fetch(`/api/inspector/event/${encodeURIComponent(eventId)}`);
        if (response.ok) {
          const data = await response.json();
          setEvent(data);
        }
      } catch (err) {
        console.error('Failed to fetch event:', err);
      } finally {
        setLoading(false);
      }
    };

    fetchEvent();
  }, [eventId]);

  if (loading) {
    return <div className="loading-spinner"><i className="iconoir-refresh-double"></i></div>;
  }

  if (!event) {
    return (
      <div className="empty-state">
        <div className="empty-state-icon"><i className="iconoir-warning-triangle"></i></div>
        <div className="empty-state-text">Event not found</div>
      </div>
    );
  }

  return (
    <EventDetailModal
      event={event}
      onClose={() => navigate('/timeline')}
    />
  );
}
