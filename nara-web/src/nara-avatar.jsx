// Nara Avatar System
// Vision and intent:
// - Naras are encountered, not looked at: a presence in fog, not a portrait.
// - Identity is the silhouette (shape language), fixed by soul hash forever.
// - Personality is mood, not anatomy: it only changes aura, motion softness,
//   and how the creature reacts to nearby attention.
// - Motion is idle life: breathing, drift, and micro parallax, never "acting".
// - Rendering should feel organic + ambient: soft edges, layered haze, and
//   a hint of lens shimmer; never sharp outlines or face-like features.
// Design constraints:
// - Deterministic: no runtime randomness beyond time-based motion.
// - Non-uncanny: no faces, eyes, or humanoid cues.
// - Expressive: personality leaks through color, response, and softness.
// - Calm: reactions are playful but never jerky or violent.

import { useRef, useEffect } from 'preact/hooks';

// --- deterministic utilities ---
export function clamp01(v) {
  return Math.max(0, Math.min(1, v));
}

function lerp(a, b, t) {
  return a + (b - a) * t;
}

// FNV-1a 32-bit for seedable streams.
function hashString32(str) {
  var h = 2166136261;
  for (var i = 0; i < str.length; i++) {
    h ^= str.charCodeAt(i);
    h = Math.imul(h, 16777619);
  }
  return h >>> 0;
}

function seedForStream(id, label) {
  return hashString32(label + ":" + id);
}

// Small PRNG for deterministic branching.
function mulberry32(seed) {
  var t = seed >>> 0;
  return function() {
    t += 0x6D2B79F5;
    var r = Math.imul(t ^ (t >>> 15), 1 | t);
    r ^= r + Math.imul(r ^ (r >>> 7), 61 | r);
    return ((r ^ (r >>> 14)) >>> 0) / 4294967296;
  };
}

function hash2d(x, y, seed) {
  var h = seed ^ Math.imul(x, 374761393) ^ Math.imul(y, 668265263);
  h = Math.imul(h ^ (h >>> 13), 1274126177);
  return ((h ^ (h >>> 16)) >>> 0) / 4294967295;
}

// Low-frequency value noise to create soft field warps.
function smoothstep(t) {
  return t * t * (3 - 2 * t);
}

function valueNoise2D(x, y, seed) {
  var xi = Math.floor(x);
  var yi = Math.floor(y);
  var xf = x - xi;
  var yf = y - yi;
  var v00 = hash2d(xi, yi, seed);
  var v10 = hash2d(xi + 1, yi, seed);
  var v01 = hash2d(xi, yi + 1, seed);
  var v11 = hash2d(xi + 1, yi + 1, seed);
  var u = smoothstep(xf);
  var v = smoothstep(yf);
  return lerp(lerp(v00, v10, u), lerp(v01, v11, u), v);
}

// --- color helpers ---
function hexToRgb(hex) {
  if (!hex || hex.length < 7) return { r: 136, g: 136, b: 136 };
  return {
    r: parseInt(hex.slice(1, 3), 16),
    g: parseInt(hex.slice(3, 5), 16),
    b: parseInt(hex.slice(5, 7), 16),
  };
}

function rgbToHsl(rgb) {
  var r = rgb.r / 255;
  var g = rgb.g / 255;
  var b = rgb.b / 255;
  var max = Math.max(r, g, b);
  var min = Math.min(r, g, b);
  var h = 0;
  var s = 0;
  var l = (max + min) / 2;

  if (max !== min) {
    var d = max - min;
    s = l > 0.5 ? d / (2 - max - min) : d / (max + min);
    switch (max) {
      case r: h = (g - b) / d + (g < b ? 6 : 0); break;
      case g: h = (b - r) / d + 2; break;
      case b: h = (r - g) / d + 4; break;
      default: break;
    }
    h /= 6;
  }
  return { h: h * 360, s: s, l: l };
}

function hslToRgb(hsl) {
  var h = (hsl.h % 360) / 360;
  var s = clamp01(hsl.s);
  var l = clamp01(hsl.l);
  if (s === 0) {
    var v = Math.round(l * 255);
    return { r: v, g: v, b: v };
  }
  var q = l < 0.5 ? l * (1 + s) : l + s - l * s;
  var p = 2 * l - q;
  var t = [h + 1/3, h, h - 1/3];
  var rgb = t.map(function(tc) {
    if (tc < 0) tc += 1;
    if (tc > 1) tc -= 1;
    if (tc < 1/6) return p + (q - p) * 6 * tc;
    if (tc < 1/2) return q;
    if (tc < 2/3) return p + (q - p) * (2/3 - tc) * 6;
    return p;
  });
  return { r: Math.round(rgb[0] * 255), g: Math.round(rgb[1] * 255), b: Math.round(rgb[2] * 255) };
}

function rgbToHex(rgb) {
  var toHex = function(v) {
    var h = v.toString(16);
    return h.length === 1 ? "0" + h : h;
  };
  return "#" + toHex(rgb.r) + toHex(rgb.g) + toHex(rgb.b);
}

function rgba(hex, alpha) {
  var c = hexToRgb(hex);
  return "rgba(" + c.r + "," + c.g + "," + c.b + "," + alpha + ")";
}

// Identity silhouette spec: the only geometry source, locked to soul.
function buildSilhouetteSpec(id) {
  var rng = mulberry32(seedForStream(id, "geometry"));
  var shapes = ["round", "tall", "wide", "ring", "cluster"];
  var shape = shapes[Math.floor(rng() * shapes.length)];
  var wobble = 0.08 + rng() * 0.12;
  var verticalBias = rng() * 0.4 - 0.2;
  var blobs = 3 + Math.floor(rng() * 4);
  var ringHole = shape === "ring" ? 0.35 + rng() * 0.2 : 0;
  return {
    shape: shape,
    wobble: wobble,
    verticalBias: verticalBias,
    blobs: blobs,
    ringHole: ringHole,
  };
}

// Grain to prevent flat, vector-like rendering.
function makeNoiseCanvas(size, id) {
  var noiseSeed = seedForStream(id, "noise");
  var canvas = document.createElement("canvas");
  canvas.width = size;
  canvas.height = size;
  var ctx = canvas.getContext("2d");
  var imageData = ctx.createImageData(size, size);
  var data = imageData.data;
  for (var y = 0; y < size; y++) {
    for (var x = 0; x < size; x++) {
      var n = hash2d(x, y, noiseSeed);
      var g = Math.floor(180 + n * 60);
      var idx = (y * size + x) * 4;
      data[idx] = g;
      data[idx + 1] = g;
      data[idx + 2] = g;
      data[idx + 3] = 18;
    }
  }
  ctx.putImageData(imageData, 0, 0);
  return canvas;
}

// AvatarCanvas: core renderer for a single Nara.
// Inputs:
// - id: deterministic soul identity (required for stable shape)
// - primary/secondary: aura colors (external system)
// - personality: sociability, agreeableness, chill
// - pointerRef: local pointer input for playful reaction
export function AvatarCanvas(props) {
  var id = props.id;
  var primary = props.primary || "#888";
  var secondary = props.secondary || primary;
  var sociability = props.sociability || 50;
  var chill = props.chill || 50;
  var agreeableness = props.agreeableness || 50;
  var buzz = props.buzz || 0;
  var size = props.size || 36;
  var pointerRef = props.pointerRef;
  var canvasRef = useRef(null);
  var specRef = useRef(null);
  var noiseRef = useRef(null);

  useEffect(() => {
    if (!id) return;
    var canvas = canvasRef.current;
    if (!canvas) return;
    var ctx = canvas.getContext("2d");
    var dpr = window.devicePixelRatio || 1;
    canvas.width = size * dpr;
    canvas.height = size * dpr;
    canvas.style.width = size + "px";
    canvas.style.height = size + "px";
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);

    specRef.current = buildSilhouetteSpec(id);
    noiseRef.current = makeNoiseCanvas(size, id);

    var fieldSeed = seedForStream(id, "field");
    var motionSeed = seedForStream(id, "motion");
    var start = performance.now();
    var raf = 0;
    var reactive = { x: 0, y: 0, vx: 0, vy: 0 };
    var pointerSmooth = { x: 0, y: 0 };
    var activeEase = 0;

    function drawFrame(now) {
      var t = (now - start) / 1000;
      var chillNorm = clamp01(chill / 100);
      var vibe = clamp01((sociability / 100 + Math.min(buzz, 12) / 12) / 2);
      var pointer = pointerRef && pointerRef.current ? pointerRef.current : { x: 0, y: 0, active: false };
      var targetActive = pointer.active ? 1 : 0;
      activeEase += (targetActive - activeEase) * 0.08;
      pointerSmooth.x += (pointer.x - pointerSmooth.x) * (pointer.active ? 0.12 : 0.05);
      pointerSmooth.y += (pointer.y - pointerSmooth.y) * (pointer.active ? 0.12 : 0.05);
      var pointerMag = Math.min(1, Math.sqrt(pointerSmooth.x * pointerSmooth.x + pointerSmooth.y * pointerSmooth.y));
      var attention = pointerMag * activeEase;
      var social = clamp01(sociability / 100);
      var agree = clamp01(agreeableness / 100);
      // Personality controls whether it approaches or avoids.
      var followBias = (social * 0.7 + agree * 0.3) * 2 - 1; // -1 = shy, +1 = curious
      var response = (0.35 + 0.65 * Math.abs(followBias)) * (1 - 0.7 * chillNorm);
      var speed = lerp(0.45, 0.12, chillNorm) * (1 + attention * 0.8);
      var drift = t * speed;
      var pulse = 0.6 + 0.4 * Math.sin(drift * 0.5 + hash2d(1, 2, motionSeed));
      var startle = attention * (0.05 + 0.02 * (1 - chillNorm));

      // Spring response: feels alive, not mechanical.
      var targetX = pointerSmooth.x * followBias * size * 0.12 * response;
      var targetY = pointerSmooth.y * followBias * size * 0.12 * response;
      var spring = 0.08 + attention * 0.06;
      var damping = 0.72 + chillNorm * 0.18;
      reactive.vx = (reactive.vx + (targetX - reactive.x) * spring) * damping;
      reactive.vy = (reactive.vy + (targetY - reactive.y) * spring) * damping;
      reactive.x += reactive.vx;
      reactive.y += reactive.vy;

      ctx.clearRect(0, 0, size, size);
      ctx.save();

      var cx = size / 2;
      var cy = size / 2;
      var baseR = size * 0.38;
      var fieldAmp = baseR * (0.04 + 0.05 * vibe + 0.03 * chillNorm);
      var fieldFreq = 1.2 + vibe * 0.6;
      var fieldCx = cx + reactive.x * 0.35;
      var fieldCy = cy + reactive.y * 0.35;

      var primaryHsl = rgbToHsl(hexToRgb(primary));
      var agreeNorm = clamp01(agreeableness / 100);
      var warmShift = lerp(-10, 14, agreeNorm);
      primaryHsl.h = (primaryHsl.h + warmShift + 360) % 360;
      var brightness = lerp(0.5, 0.72, clamp01(sociability / 100));
      primaryHsl.l = lerp(primaryHsl.l, brightness, 0.35);
      var dominant = rgbToHex(hslToRgb(primaryHsl));
      var shadowHsl = { h: primaryHsl.h, s: primaryHsl.s * 0.9, l: primaryHsl.l * 0.55 };
      var highlightHsl = { h: (primaryHsl.h + 6) % 360, s: primaryHsl.s * 0.8, l: clamp01(primaryHsl.l + 0.18) };
      var shadow = rgbToHex(hslToRgb(shadowHsl));
      var highlight = rgbToHex(hslToRgb(highlightHsl));

      // Field glow: "presence" that gently ripples.
      var grad = ctx.createRadialGradient(fieldCx, fieldCy, baseR * 0.2, fieldCx, fieldCy, baseR * 1.2);
      grad.addColorStop(0, rgba(dominant, 0.65));
      grad.addColorStop(0.7, rgba(shadow, 0.28));
      grad.addColorStop(1, rgba(dominant, 0));

      ctx.shadowColor = rgba(dominant, 0.35);
      ctx.shadowBlur = size * (0.16 + vibe * 0.1);
      ctx.fillStyle = grad;
      ctx.beginPath();
      for (var i = 0; i <= 48; i++) {
        var a = (i / 48) * Math.PI * 2;
        var nx = Math.cos(a) * fieldFreq + drift * 0.4;
        var ny = Math.sin(a) * fieldFreq + drift * 0.4;
        var n = valueNoise2D(nx, ny, fieldSeed);
        var r = baseR + (n - 0.5) * fieldAmp * pulse;
        var x = cx + r * Math.cos(a);
        var y = cy + r * Math.sin(a);
        if (i === 0) ctx.moveTo(x, y);
        else ctx.lineTo(x, y);
      }
      ctx.closePath();
      ctx.fill();
      ctx.restore();

      // Silhouette body: identity container, never face-like.
      var spec = specRef.current;
      var bodyScale = lerp(0.9, 1.05, pulse) * (1 + startle * 0.25);
      bodyScale *= 1 - Math.abs(reactive.x) / size * 0.08;
      var bob = Math.sin(drift * 0.6 + 1.2) * size * (0.02 + 0.02 * (1 - chillNorm));
      bob += Math.sin(drift * 1.8 + 2.1) * size * startle;
      var driftRot = Math.sin(drift * 0.25 + 0.9) * 0.08;
      driftRot += Math.sin(drift * 1.1 + 0.2) * startle * 0.18;
      var bodyCx = cx + reactive.x;
      var bodyCy = cy + bob + size * spec.verticalBias * 0.2 + reactive.y;
      bodyCx += Math.cos(drift * 0.9 + 0.5) * size * startle;
      var baseW = size * 0.34;
      var baseH = size * 0.34;
      if (spec.shape === "tall") {
        baseH *= 1.35;
        baseW *= 0.8;
      } else if (spec.shape === "wide") {
        baseW *= 1.35;
        baseH *= 0.75;
      } else if (spec.shape === "ring") {
        baseW *= 1.1;
        baseH *= 1.1;
      } else if (spec.shape === "cluster") {
        baseW *= 1.1;
        baseH *= 1.05;
      }

      ctx.save();
      ctx.translate(bodyCx, bodyCy);
      ctx.rotate(driftRot);
      ctx.scale(bodyScale, bodyScale);
      ctx.shadowColor = rgba(shadow, 0.35);
      ctx.shadowBlur = size * (0.18 + 0.1 * (1 - chillNorm));

      var bodyGrad = ctx.createRadialGradient(0, -baseH * 0.2, baseW * 0.2, 0, 0, baseW * 1.2);
      bodyGrad.addColorStop(0, rgba(highlight, 0.6));
      bodyGrad.addColorStop(0.5, rgba(dominant, 0.85));
      bodyGrad.addColorStop(1, rgba(shadow, 0.9));
      ctx.fillStyle = bodyGrad;

      function buildBodyPath() {
        ctx.beginPath();
        if (spec.shape === "ring") {
          ctx.ellipse(0, 0, baseW, baseH, 0, 0, Math.PI * 2);
        } else if (spec.shape === "cluster") {
          for (var b = 0; b < spec.blobs; b++) {
            var ang = (b / spec.blobs) * Math.PI * 2;
            var rad = baseW * 0.35;
            var bx = Math.cos(ang) * rad * (0.6 + 0.2 * Math.sin(drift + b));
            var by = Math.sin(ang) * rad * 0.6;
            var br = baseW * (0.35 + 0.08 * Math.sin(drift * 0.8 + b));
            ctx.moveTo(bx + br, by);
            ctx.ellipse(bx, by, br, br * 0.9, 0, 0, Math.PI * 2);
          }
        } else {
          var steps = 24;
          for (var s = 0; s <= steps; s++) {
            var a = (s / steps) * Math.PI * 2;
            var wob = Math.sin(a * (3 + spec.wobble * 8) + drift * 0.4) * spec.wobble;
            var rx = baseW * (1 + wob);
            var ry = baseH * (1 + wob * 0.7);
            var x = Math.cos(a) * rx;
            var y = Math.sin(a) * ry;
            if (s === 0) ctx.moveTo(x, y);
            else ctx.lineTo(x, y);
          }
        }
        ctx.closePath();
      }

      buildBodyPath();
      ctx.fill();

      // Chromatic aberration: faint spectral softness, no outlines.
      ctx.save();
      ctx.globalCompositeOperation = "screen";
      ctx.globalAlpha = 0.22;
      ctx.translate(0.6, -0.4);
      ctx.fillStyle = rgba(highlight, 0.5);
      buildBodyPath();
      ctx.fill();
      ctx.restore();

      ctx.save();
      ctx.globalCompositeOperation = "screen";
      ctx.globalAlpha = 0.18;
      ctx.translate(-0.6, 0.4);
      ctx.fillStyle = rgba(shadow, 0.45);
      buildBodyPath();
      ctx.fill();
      ctx.restore();

      if (spec.shape === "ring") {
        ctx.globalCompositeOperation = "destination-out";
        ctx.beginPath();
        ctx.ellipse(0, 0, baseW * spec.ringHole, baseH * spec.ringHole, 0, 0, Math.PI * 2);
        ctx.fill();
        ctx.globalCompositeOperation = "source-over";
      }
      ctx.restore();

      // Partial occlusion (fog): you never see the whole creature.
      ctx.save();
      var fogGrad = ctx.createLinearGradient(0, size * 0.38 + reactive.y * 0.2, 0, size);
      fogGrad.addColorStop(0, "rgba(255,255,255,0)");
      fogGrad.addColorStop(0.6, "rgba(255,255,255,0.22)");
      fogGrad.addColorStop(1, "rgba(255,255,255,0.45)");
      ctx.fillStyle = fogGrad;
      ctx.fillRect(0, size * 0.38, size, size * 0.62);
      ctx.restore();

      // Lens flare whisper: soft, dreamlike presence.
      ctx.save();
      ctx.globalCompositeOperation = "screen";
      ctx.globalAlpha = 0.25 + startle * 0.15;
      var flareX = cx - size * 0.12 - reactive.x * 0.3;
      var flareY = cy - size * 0.18 + bob * 0.4 - reactive.y * 0.2;
      var flare = ctx.createRadialGradient(flareX, flareY, 0, flareX, flareY, size * 0.18);
      flare.addColorStop(0, rgba(highlight, 0.6));
      flare.addColorStop(0.6, rgba(highlight, 0.15));
      flare.addColorStop(1, "rgba(255,255,255,0)");
      ctx.fillStyle = flare;
      ctx.beginPath();
      ctx.arc(flareX, flareY, size * 0.18, 0, Math.PI * 2);
      ctx.fill();
      ctx.restore();

      // Grain + bloom veil
      if (noiseRef.current) {
        ctx.save();
        ctx.globalCompositeOperation = "soft-light";
        ctx.globalAlpha = 0.35;
        ctx.drawImage(noiseRef.current, 0, 0);
        ctx.restore();
      }

      raf = requestAnimationFrame(drawFrame);
    }

    raf = requestAnimationFrame(drawFrame);
    return function() {
      cancelAnimationFrame(raf);
    };
  }, [id, primary, secondary, sociability, chill, agreeableness, buzz, size, pointerRef]);

  return (
    <canvas className="nara-avatar" ref={canvasRef} />
  );
}

// Also expose on window for backward compatibility with profile.html
if (typeof window !== 'undefined') {
  window.NaraAvatar = {
    AvatarCanvas: AvatarCanvas,
    clamp01: clamp01,
  };
}
