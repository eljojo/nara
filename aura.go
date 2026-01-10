package nara

import (
	"encoding/binary"
	"hash/fnv"
	"math"
)

// Aura holds visual identity information derived from personality and soul
type Aura struct {
	Primary   string `json:"primary"`   // Main aura color (HEX)
	Secondary string `json:"secondary"` // Border/accent color (HEX)
	// Future fields: Glow int, Pulse bool, Intensity float64, etc.
}

type PaletteModifier int

const (
	ModDefault  PaletteModifier = iota
	ModWarmBias                 // nudges towards warm hues
	ModCoolBias                 // nudges towards cool hues
	ModNoir                     // darker, lower chroma
	ModNeon                     // higher chroma, a bit brighter
)

type Illuminant struct {
	A float64 // OKLab a axis bias (green<->red)
	B float64 // OKLab b axis bias (blue<->yellow)
}

var (
	IllDaylight = Illuminant{A: 0.00, B: 0.00}   // neutral
	IllTungsten = Illuminant{A: 0.02, B: 0.06}   // warm indoor
	IllSodium   = Illuminant{A: 0.01, B: 0.10}   // streetlight orange
	IllLED      = Illuminant{A: -0.02, B: -0.03} // cool LED
	IllMoon     = Illuminant{A: -0.01, B: -0.06} // cool night
)

type ColorPair struct {
	Primary   RGB
	Secondary RGB
}

type RGB struct{ R, G, B uint8 }

func (c RGB) Hex() string {
	const hexd = "0123456789ABCDEF"
	out := make([]byte, 7)
	out[0] = '#'
	out[1] = hexd[c.R>>4]
	out[2] = hexd[c.R&0xF]
	out[3] = hexd[c.G>>4]
	out[4] = hexd[c.G&0xF]
	out[5] = hexd[c.B>>4]
	out[6] = hexd[c.B&0xF]
	return string(out)
}

// computeAura generates aura with primary and secondary colors based on personality and soul
// Uses OKLCH color space with personality-controlled aesthetics
// Thread-safe: locks Me.mu to read personality
func (ln *LocalNara) computeAura() Aura {
	// Lock to safely read personality and name (prevents data race)
	ln.Me.mu.Lock()
	p := ln.Me.Status.Personality
	name := ln.Me.Name
	observation := ln.Me.Status.Observations[ln.Me.Name]
	uptime := uint64(observation.LastSeen - observation.LastRestart)
	ln.Me.mu.Unlock()

	soul := ln.Soul

	// ID = soul + name for unique color identity
	id := soul + name

	// Determine palette modifier based on personality traits
	mod := choosePaletteModifier(p)

	// Generate color pair using OKLCH color science
	colors := NaraColorsFromStringWithUptime(id, p, mod, uptime)

	return Aura{
		Primary:   colors.Primary.Hex(),
		Secondary: colors.Secondary.Hex(),
	}
}

// choosePaletteModifier selects a palette modifier based on personality
func choosePaletteModifier(p NaraPersonality) PaletteModifier {
	sociability := p.Sociability
	chill := p.Chill

	// Neon lovers (high sociability, low chill) - vibrant, high contrast
	if sociability > 70 && chill < 40 {
		return ModNeon
	}

	// Very chill - muted, natural
	if chill > 70 {
		return ModNoir
	}

	// Cool-leaning for balanced/withdrawn types
	if sociability < 40 {
		return ModCoolBias
	}

	// Warm-leaning for social but not extreme
	if sociability > 60 && chill > 40 {
		return ModWarmBias
	}

	return ModDefault
}

// --- Harmony logic (agreeableness-driven) ---

type harmonyMode int

const (
	hAnalogous harmonyMode = iota
	hSplitComplement
	hTriadic
	hComplement
)

func chooseHarmony(agreeableness float64, h uint32) harmonyMode {
	// High agreeableness: analogous more often.
	// Low agreeableness: more contrast (split/triad/complement).
	r := hashToUnit(h, 0x51ED270B)
	switch {
	case agreeableness >= 0.75:
		if r < 0.85 {
			return hAnalogous
		}
		return hSplitComplement
	case agreeableness >= 0.45:
		if r < 0.55 {
			return hSplitComplement
		}
		return hAnalogous
	default:
		if r < 0.40 {
			return hTriadic
		}
		return hComplement
	}
}

func harmonyOffsetDegrees(mode harmonyMode, h uint32) float64 {
	// Add small jitter so palettes feel "chosen" rather than mathematically perfect.
	j := (hashToUnit(h, 0xC0FFEE) - 0.5) * 14 // +/-7°
	switch mode {
	case hAnalogous:
		// cozy neighbors
		return 28 + j
	case hSplitComplement:
		// energetic but not harsh
		return 150 + j
	case hTriadic:
		// playful + distinct
		return 120 + j
	case hComplement:
		// high contrast
		return 180 + j
	default:
		return 150 + j
	}
}

func adjustLightnessAway(L float64, base float64, minDelta float64, h uint32) float64 {
	// Deterministic direction choice
	dir := -1.0
	if hashToUnit(h, 0xBADC0DE) > 0.5 {
		dir = 1.0
	}
	if math.Abs(L-base) >= minDelta {
		return clamp(L, 0.18, 0.88)
	}
	L = base + dir*minDelta
	// If we hit a bound, flip.
	if L < 0.18 || L > 0.88 {
		L = base - dir*minDelta
	}
	return clamp(L, 0.18, 0.88)
}

// --- OKLCH -> sRGB with illuminant (memory tint) and gamut mapping ---

func oklchToSRGBGamutMappedIlluminated(L, C, Hdeg float64, ill Illuminant, strength float64) RGB {
	hrad := Hdeg * (math.Pi / 180.0)
	a0 := C * math.Cos(hrad)
	b0 := C * math.Sin(hrad)

	shiftA := ill.A * strength
	shiftB := ill.B * strength

	// Reduce only the chroma component until in gamut; keep the illuminant shift intact.
	k := 1.0
	for i := 0; i < 14; i++ {
		a := k*a0 + shiftA
		b := k*b0 + shiftB
		r, g, bb := oklabToLinearSRGB(L, a, b)
		if inGamut01(r) && inGamut01(g) && inGamut01(bb) {
			return linearToSRGB8(r, g, bb)
		}
		k *= 0.88
	}

	// Fallback: clamp (rare)
	a := k*a0 + shiftA
	b := k*b0 + shiftB
	r, g, bb := oklabToLinearSRGB(L, a, b)
	return linearToSRGB8(clamp01(r), clamp01(g), clamp01(bb))
}

func aFromCH(C, Hdeg float64) float64 {
	hrad := Hdeg * (math.Pi / 180.0)
	return C * math.Cos(hrad)
}
func bFromCH(C, Hdeg float64) float64 {
	hrad := Hdeg * (math.Pi / 180.0)
	return C * math.Sin(hrad)
}

func oklabToLinearSRGB(L, a, b float64) (r, g, bb float64) {
	// Björn Ottosson OKLab -> linear sRGB
	l_ := L + 0.3963377774*a + 0.2158037573*b
	m_ := L - 0.1055613458*a - 0.0638541728*b
	s_ := L - 0.0894841775*a - 1.2914855480*b

	l := l_ * l_ * l_
	m := m_ * m_ * m_
	s := s_ * s_ * s_

	r = +4.0767416621*l - 3.3077115913*m + 0.2309699292*s
	g = -1.2684380046*l + 2.6097574011*m - 0.3413193965*s
	bb = -0.0041960863*l - 0.7034186147*m + 1.7076147010*s
	return
}

func linearToSRGB8(r, g, b float64) RGB {
	return RGB{R: toSRGB8(r), G: toSRGB8(g), B: toSRGB8(b)}
}

func toSRGB8(x float64) uint8 {
	x = clamp01(x)
	var y float64
	if x <= 0.0031308 {
		y = 12.92 * x
	} else {
		y = 1.055*math.Pow(x, 1.0/2.4) - 0.055
	}
	v := int(math.Round(y * 255.0))
	if v < 0 {
		v = 0
	} else if v > 255 {
		v = 255
	}
	return uint8(v)
}

func inGamut01(x float64) bool { return x >= 0 && x <= 1 }

// --- Hash helpers ---

func fnv32a(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}

func hashToUnit(h uint32, salt uint32) float64 {
	// Cheap deterministic mixing -> [0,1)
	x := h ^ salt
	x ^= x >> 16
	x *= 0x7feb352d
	x ^= x >> 15
	x *= 0x846ca68b
	x ^= x >> 16
	return float64(x) / float64(^uint32(0))
}

func fract(x float64) float64 { return x - math.Floor(x) }

// --- Small math utils ---

func wrapHue(h float64) float64 {
	h = math.Mod(h, 360.0)
	if h < 0 {
		h += 360.0
	}
	return h
}
func clamp01(x float64) float64 { return clamp(x, 0, 1) }
func clamp(x, lo, hi float64) float64 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

// Optional: if you want a 32-bit hash from bytes elsewhere:
func Uint32FromBytes(b []byte) uint32 {
	if len(b) < 4 {
		var tmp [4]byte
		copy(tmp[:], b)
		return binary.LittleEndian.Uint32(tmp[:])
	}
	return binary.LittleEndian.Uint32(b[:4])
}

func uptimeFactorSeconds(u uint64) float64 {
	// Map ~0..30 days to 0..1 (log curve)
	const max = 30 * 24 * 3600
	x := float64(u)
	return clamp01(math.Log1p(x) / math.Log1p(max))
}

func chooseIlluminant(h uint32, p NaraPersonality, mod PaletteModifier, uptimeSeconds uint64) (Illuminant, float64) {
	a := clamp01(float64(p.Agreeableness) / 100.0)
	s := clamp01(float64(p.Sociability) / 100.0)
	c := clamp01(float64(p.Chill) / 100.0)
	u := uptimeFactorSeconds(uptimeSeconds)

	// Warmth: uptime + chill pull warm; sociability pulls cool (LED/club)
	warm := clamp01(0.55*u + 0.35*c + 0.10*(1.0-s))
	cool := clamp01(0.55*s + 0.25*(1.0-c) + 0.20*(1.0-u))

	// Modifier nudges
	switch mod {
	case ModWarmBias:
		warm = clamp01(warm + 0.15)
	case ModCoolBias:
		cool = clamp01(cool + 0.15)
	case ModNoir:
		// noir feels like night + sodium/tungsten
		warm = clamp01(warm + 0.08)
	case ModNeon:
		// neon feels like LED-heavy spaces
		cool = clamp01(cool + 0.10)
	}

	// Agreeableness: more agreeable => closer to neutral daylight (less “filter”)
	neutralPull := 0.35 + 0.45*a // 0.35..0.80

	// Deterministic “environment lottery” between warm indoor vs streetlight vs cool LED vs moonlight.
	r := hashToUnit(h, 0x1A11A7ED)
	var ill Illuminant
	if warm >= cool {
		ill = IllTungsten
		if r < 0.25+0.35*u {
			ill = IllSodium // older naras drift into sodium-night vibes more often
		}
	} else {
		ill = IllLED
		if r < 0.20+0.25*(1.0-u) {
			ill = IllMoon // younger / rebooted naras can feel “cold”
		}
	}

	// Strength: subtle, but present. Uptime increases “memory tint”.
	// Agreeableness reduces tint (socially “neutral”)
	strength := clamp(0.12+0.55*u+0.15*(1.0-c), 0.08, 0.80)
	strength *= (1.0 - 0.55*a)

	// Pull toward daylight (keeps it tasteful, Pantone-ish)
	ill = lerpIlluminant(ill, IllDaylight, neutralPull)

	return ill, strength
}

func lerpIlluminant(a, b Illuminant, t float64) Illuminant {
	return Illuminant{
		A: a.A + (b.A-a.A)*t,
		B: a.B + (b.B-a.B)*t,
	}
}

// --- Public entrypoints (with uptime / illuminant) ---

func NaraColorsFromStringWithUptime(id string, p NaraPersonality, mod PaletteModifier, uptimeSeconds uint64) ColorPair {
	h := fnv32a(id)
	return NaraColorsFromHashWithUptime(h, p, mod, uptimeSeconds)
}

func NaraColorsFromHashWithUptime(h uint32, p NaraPersonality, mod PaletteModifier, uptimeSeconds uint64) ColorPair {
	ill, strength := chooseIlluminant(h, p, mod, uptimeSeconds)
	return naraColorsCore(h, p, mod, ill, strength)
}

// naraColorsCore is the same palette logic as NaraColorsFromHash, but “remembered under light”.
// We apply an illuminant bias in OKLab during OKLCH->sRGB conversion (with gamut-mapped chroma).
func naraColorsCore(h uint32, p NaraPersonality, mod PaletteModifier, ill Illuminant, illStrength float64) ColorPair {
	a := clamp01(float64(p.Agreeableness) / 100.0)
	s := clamp01(float64(p.Sociability) / 100.0)
	c := clamp01(float64(p.Chill) / 100.0)

	// 1) Base hue from hash (0..360), then golden-angle shuffle for better spread.
	baseHue := float64(h%360) + fract(hashToUnit(h, 0xA2C2A1))*137.50776405003785
	baseHue = wrapHue(baseHue)

	// 2) Personality -> OKLCH "style"
	L := 0.52 + 0.18*c - 0.06*s     // 0.28..0.76ish
	C := 0.06 + 0.22*s + 0.05*(1-c) // 0.06..0.33ish

	harmony := chooseHarmony(a, h)

	// 3) Modifier tweaks (small, art-directable)
	switch mod {
	case ModWarmBias:
		baseHue = wrapHue(baseHue + 12)
	case ModCoolBias:
		baseHue = wrapHue(baseHue - 12)
	case ModNoir:
		L -= 0.12
		C *= 0.75
	case ModNeon:
		L += 0.04
		C *= 1.18
	}

	L = clamp(L, 0.18, 0.88)
	C = clamp(C, 0.02, 0.40)

	// 4) Primary color (remembered under light)
	primary := oklchToSRGBGamutMappedIlluminated(L, C, baseHue, ill, illStrength)

	// 5) Secondary color: harmony strategy + ensure clear separation
	secondaryHue := wrapHue(baseHue + harmonyOffsetDegrees(harmony, h))
	secondaryL := L
	secondaryC := C

	secondaryC = clamp(secondaryC*(0.78+0.44*(1-s)), 0.02, 0.38)

	minDeltaL := 0.10 + 0.06*(1-c)
	if s > 0.65 {
		minDeltaL += 0.03
	}
	secondaryL = adjustLightnessAway(secondaryL, L, minDeltaL, h)

	secondary := oklchToSRGBGamutMappedIlluminated(secondaryL, secondaryC, secondaryHue, ill, illStrength)

	return ColorPair{Primary: primary, Secondary: secondary}
}
