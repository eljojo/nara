// nara - Shooting Stars (for social events)

import { Fragment } from 'preact';
import { useState, useEffect, useRef } from 'preact/hooks';

function Sparkle({ x, y, color, delay }) {
  return <div className={`sparkle ${color}`} style={{ left: x, top: y, animationDelay: `${delay}ms` }} />;
}

function ShootingStar({ event, onComplete }) {
  const [sparkles, setSparkles] = useState([]);
  const sparkleId = useRef(0);
  const startY = useRef(Math.random() * 60 + 10);

  useEffect(() => {
    const timer = setTimeout(onComplete, 2500);
    const colors = ['gold', 'pink', 'white', 'purple'];

    const sparkleInterval = setInterval(() => {
      const elapsed = Date.now() % 2000;
      const progress = elapsed / 2000;

      if (progress > 0.1 && progress < 0.8) {
        const x = progress * (window.innerWidth + 200) - 100;
        const y = (startY.current / 100) * window.innerHeight - (progress * 80);

        const newSparkle = {
          id: sparkleId.current++,
          x: x + (Math.random() - 0.5) * 40,
          y: y + (Math.random() - 0.5) * 30,
          color: colors[Math.floor(Math.random() * colors.length)],
          delay: Math.random() * 50,
        };
        setSparkles(prev => [...prev.slice(-6), newSparkle]);
      }
    }, 150);

    return () => {
      clearTimeout(timer);
      clearInterval(sparkleInterval);
    };
  }, []);

  return (
    <Fragment>
      {sparkles.map(s => <Sparkle key={s.id} x={s.x} y={s.y} color={s.color} delay={s.delay} />)}
      <div className="shooting-star burst" style={{ top: `${startY.current}%`, left: '-100px' }}>
        <span className="actor">{event.actor}</span>
        <span className="arrow">→</span>
        <span className="target">{event.target}</span>
        <span style={{ marginLeft: '8px', opacity: 0.9 }}>{event.message}</span>
      </div>
    </Fragment>
  );
}

export function ShootingStarContainer() {
  const [stars, setStars] = useState([]);
  const lastStarTime = useRef(0);
  const MIN_INTERVAL_MS = 150000;
  const MAX_INTERVAL_MS = 300000;

  useEffect(() => {
    const eventSource = new EventSource('/events');

    eventSource.addEventListener('social', (e) => {
      const data = JSON.parse(e.data);
      if (data.social) {
        const now = Date.now();
        const cooldown = MIN_INTERVAL_MS + Math.random() * (MAX_INTERVAL_MS - MIN_INTERVAL_MS);

        if (now - lastStarTime.current >= cooldown) {
          lastStarTime.current = now;
          setStars(prev => [...prev, {
            id: now + Math.random(),
            event: {
              actor: data.social.actor,
              target: data.social.target,
              message: data.social.reason,
            }
          }]);
        }
      }
    });

    return () => eventSource.close();
  }, []);

  return (
    <div className="shooting-star-container">
      {stars.map(({ id, event }) => (
        <ShootingStar key={id} event={event} onComplete={() => setStars(prev => prev.filter(s => s.id !== id))} />
      ))}
    </div>
  );
}
