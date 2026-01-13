// nara - Simple Path Router (using History API)

import { useState, useEffect } from 'preact/hooks';

const locationListeners = new Set();

function broadcastLocationChange() {
  const path = window.location.pathname;
  locationListeners.forEach(listener => listener(path));
}

export function globalNavigate(to) {
  window.history.pushState(null, '', to);
  broadcastLocationChange();
}

window.addEventListener('popstate', broadcastLocationChange);

export function useLocation() {
  const [location, setLocation] = useState(window.location.pathname);

  useEffect(() => {
    locationListeners.add(setLocation);
    return () => locationListeners.delete(setLocation);
  }, []);

  return [location, globalNavigate];
}

export function matchRoute(pattern, path) {
  const patternParts = pattern.split('/').filter(Boolean);
  const pathParts = path.split('/').filter(Boolean);

  if (patternParts.length !== pathParts.length) return null;

  const params = {};
  for (let i = 0; i < patternParts.length; i++) {
    if (patternParts[i].startsWith(':')) {
      params[patternParts[i].slice(1)] = decodeURIComponent(pathParts[i]);
    } else if (patternParts[i] !== pathParts[i]) {
      return null;
    }
  }
  return params;
}

export function Link({ href, children, className, style }) {
  const handleClick = (e) => {
    e.preventDefault();
    globalNavigate(href);
  };
  return <a href={href} onClick={handleClick} className={className} style={style}>{children}</a>;
}
