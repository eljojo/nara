// nara - Utility Functions

import dayjs from 'dayjs';
import relativeTime from 'dayjs/plugin/relativeTime';

dayjs.extend(relativeTime);

export function stringToColor(str) {
  let hash = 0;
  for (let i = 0; i < str.length; i++) {
    hash = str.charCodeAt(i) + ((hash << 5) - hash);
  }
  let colour = '#';
  for (let i = 0; i < 3; i++) {
    const value = (hash >> (i * 8)) & 0xFF;
    colour += ('00' + value.toString(16)).substr(-2);
  }
  return colour;
}

export function stringToHue(str) {
  let hash = 0;
  for (let i = 0; i < str.length; i++) {
    hash = str.charCodeAt(i) + ((hash << 5) - hash);
  }
  return Math.abs(hash) % 360;
}

export function seededRandom(seed) {
  const x = Math.sin(seed) * 10000;
  return x - Math.floor(x);
}

export function timeAgo(timestamp) {
  if (!timestamp || timestamp === 0) return 'never';
  const ts = timestamp > 10000000000000 ? timestamp / 1000000000 : timestamp;
  const seconds = dayjs().unix() - ts;

  if (seconds < 60) return `${Math.round(seconds)}s ago`;
  if (seconds < 3600) return `${Math.round(seconds / 60)}m ago`;
  if (seconds < 86400) return `${Math.round(seconds / 3600)}h ago`;
  return `${Math.round(seconds / 86400)}d ago`;
}

export function formatTimestamp(timestamp) {
  if (!timestamp || timestamp === 0) return 'never';
  const ts = timestamp > 10000000000000 ? timestamp / 1000000000 : timestamp;
  return dayjs.unix(ts).format('MMM D, YYYY HH:mm:ss');
}

export function formatDuration(seconds) {
  if (!seconds || seconds < 0) return '0s';
  if (seconds < 60) return `${Math.round(seconds)}s`;
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

export function formatDateRange(startTime, endTime, ongoing) {
  const start = dayjs.unix(startTime);
  const end = ongoing ? dayjs() : dayjs.unix(endTime);
  const startStr = start.format('MMM D, YYYY HH:mm');
  const endStr = ongoing ? 'now' : end.format('MMM D, YYYY HH:mm');
  return `${startStr} → ${endStr}`;
}

export { dayjs };
