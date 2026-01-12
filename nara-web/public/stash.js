const { useState, useEffect, useRef } = React;

function StashManager() {
  const [status, setStatus] = useState(null);
  const [loading, setLoading] = useState(true);
  const [message, setMessage] = useState({ text: '', type: '' });
  const editorRef = useRef(null);
  const containerRef = useRef(null);

  useEffect(() => {
    fetchStatus();
    const interval = setInterval(fetchStatus, 5000); // Refresh every 5 seconds
    return () => clearInterval(interval);
  }, []);

  useEffect(() => {
    if (containerRef.current && !editorRef.current && status) {
      // Initialize JSONEditor
      const options = {
        mode: 'tree',
        modes: ['tree', 'code', 'form', 'text', 'view'],
        onChangeText: () => {
          // Editor content changed
        }
      };
      editorRef.current = new JSONEditor(containerRef.current, options);

      // Use existing stash data, or provide sample placeholder
      let initialData = {};
      if (status.my_stash && status.my_stash.data) {
        initialData = status.my_stash.data;
      } else {
        // Sample placeholder data
        initialData = {
          notes: "My personal notes",
          preferences: {
            theme: "dark",
            notifications: true
          },
          bookmarks: [
            "https://example.com",
            "https://github.com"
          ]
        };
      }

      editorRef.current.set(initialData);
    }
  }, [status]);

  async function fetchStatus() {
    try {
      const response = await fetch('/api/stash/status');
      const data = await response.json();
      setStatus(data);
      setLoading(false);

      // Update editor if it exists
      if (editorRef.current && data.my_stash) {
        editorRef.current.update(data.my_stash.data || {});
      }
    } catch (error) {
      console.error('Failed to fetch status:', error);
      setLoading(false);
    }
  }

  async function handleUpdate() {
    if (!editorRef.current) return;

    try {
      const data = editorRef.current.get();
      const response = await fetch('/api/stash/update', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data)
      });

      const result = await response.json();
      showMessage(result.message || 'Stash updated successfully', 'success');
      setTimeout(fetchStatus, 1000);
    } catch (error) {
      showMessage('Failed to update stash: ' + error.message, 'error');
    }
  }

  async function handleRecover() {
    try {
      const response = await fetch('/api/stash/recover', {
        method: 'POST'
      });

      const result = await response.json();
      showMessage(result.message || 'Recovery initiated', 'success');
      setTimeout(fetchStatus, 2000);
    } catch (error) {
      showMessage('Failed to initiate recovery: ' + error.message, 'error');
    }
  }

  function handleClear() {
    if (editorRef.current && confirm('Clear all stash data?')) {
      editorRef.current.set({});
    }
  }

  function showMessage(text, type) {
    setMessage({ text, type });
    setTimeout(() => setMessage({ text: '', type: '' }), 5000);
  }

  function formatBytes(bytes) {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return (bytes / Math.pow(k, i)).toFixed(2) + ' ' + sizes[i];
  }

  function formatTimestamp(ts) {
    if (!ts) return 'Never';
    dayjs.extend(dayjs_plugin_relativeTime);
    return dayjs.unix(ts).fromNow();
  }

  if (loading) {
    return (
      <div className="container">
        <div className="header">
          <h1>📦 Stash Manager</h1>
          <a href="/">← Back to Naras</a>
        </div>
        <div className="card">
          <p>Loading...</p>
        </div>
      </div>
    );
  }

  const metrics = (status && status.metrics) || {};
  const myStash = status && status.my_stash;
  const confidants = (status && status.confidants) || [];
  const targetCount = (status && status.target_count) || 2;

  return (
    <div className="container">
      <div className="header">
        <h1>📦 Stash Manager</h1>
        <a href="/">← Back to Naras</a>
      </div>

      {/* Metrics Overview */}
      <div className="card">
        <h2>📊 Metrics</h2>
        <div className="metrics-grid">
          <div className="metric-box">
            <div className="metric-value">{metrics.stashes_stored || 0}</div>
            <div className="metric-label">Stashes Stored</div>
          </div>
          <div className="metric-box">
            <div className="metric-value">{formatBytes(metrics.total_bytes || 0)}</div>
            <div className="metric-label">Total Size</div>
          </div>
          <div className="metric-box">
            <div className="metric-value">{confidants.length}/{targetCount}</div>
            <div className="metric-label">Confidants</div>
          </div>
          <div className="metric-box">
            <div className="metric-value">{metrics.storage_limit || 0}</div>
            <div className="metric-label">Storage Limit</div>
          </div>
        </div>
      </div>

      {/* My Stash Status */}
      <div className="card">
        <h2>💾 My Stash</h2>
        {myStash ? (
          <div>
            <div className="status-row">
              <span className="status-label">Last Updated:</span>
              <span className="status-value">{formatTimestamp(myStash.timestamp)}</span>
            </div>
            <div className="status-row">
              <span className="status-label">Confidants:</span>
              <span className="status-value">
                {confidants.length} / {targetCount}
                {confidants.length >= targetCount ? ' ✅' : ' ⚠️'}
              </span>
            </div>
          </div>
        ) : (
          <p style={{color: '#666'}}>No stash data yet. Create one below!</p>
        )}
      </div>

      {/* JSON Editor */}
      <div className="card">
        <h2>✏️ Edit Stash Data</h2>
        <div className="editor-container" ref={containerRef}></div>
        <div style={{marginTop: '10px'}}>
          <button className="button" onClick={handleUpdate}>
            💾 Save & Distribute
          </button>
          <button className="button secondary" onClick={handleClear}>
            🗑️ Clear
          </button>
          <button className="button secondary" onClick={handleRecover}>
            🔄 Trigger Recovery
          </button>
        </div>
        {message.text && (
          <div className={`message ${message.type}`}>
            {message.text}
          </div>
        )}
      </div>

      {/* Confidants List */}
      <div className="card">
        <h2>🤝 Confidants ({confidants.length})</h2>
        {confidants.length > 0 ? (
          <div className="confidant-list">
            {confidants.map((c, i) => (
              <div key={i} className="confidant-item">
                <div className="confidant-name">{c.name}</div>
                <div className="confidant-detail">
                  {c.memory_mode ? `Memory: ${c.memory_mode}` : 'Status: confirmed'}
                </div>
              </div>
            ))}
          </div>
        ) : (
          <p style={{color: '#666'}}>
            No confidants yet. They'll be selected automatically once you have stash data.
          </p>
        )}
      </div>

      {/* Storage Metrics */}
      <div className="card">
        <h2>💽 Storage (for others)</h2>
        <div className="status-row">
          <span className="status-label">Stashes Stored:</span>
          <span className="status-value">{metrics.stashes_stored || 0}</span>
        </div>
        <div className="status-row">
          <span className="status-label">Total Size:</span>
          <span className="status-value">{formatBytes(metrics.total_bytes || 0)}</span>
        </div>
        <div className="status-row">
          <span className="status-label">Storage Limit:</span>
          <span className="status-value">{metrics.storage_limit || 0} stashes</span>
        </div>
        <div className="status-row">
          <span className="status-label">Ghost Prunings:</span>
          <span className="status-value" title="Stashes removed when owner offline 7+ days">
            {metrics.eviction_count || 0}
          </span>
        </div>
      </div>
    </div>
  );
}

ReactDOM.render(<StashManager />, document.getElementById('stash_container'));
