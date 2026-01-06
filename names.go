package nara

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"sort"
	"strings"
)

var adjectives = []string{
	"mystic", "dandelion", "quirky", "glitter", "neon", "fuzzy", "cosmic", "bouncy",
	"mellow", "spicy", "frozen", "velvet", "salty", "grumpy", "dizzy", "funky", "electric", "pixel",
	"golden", "silver", "crimson", "azure", "emerald", "vivid", "silent", "noisy", "sparkly", "cyber",
	"brave", "calm", "wild", "tame", "fast", "slow", "heavy", "light", "clumsy", "fancy", "magical",
	"dreamy", "clover", "vibe", "chill", "sociable", "agreeable", "buzzing", "nara", "stretchy", "stinky",
	"wobbly", "tiny", "giga", "mega", "ultra", "hyper", "turbo", "digital", "analog", "shimmering",
}

var nouns = []string{
	"pizza", "burger", "icecream", "donut", "cupcake", "lollipop", "candy", "chocolate",
	"popcorn", "salt", "egg", "waffle", "pancake", "butter", "clover", "flower",
	"cat", "dog", "fox", "owl", "bear", "wolf", "tiger", "lion", "panda", "otter", "hamster",
	"robot", "alien", "ghost", "wizard", "ninja", "pirate", "zombie", "vampire", "astronaut", "nara",
	"mushroom", "crystal", "comet", "star", "moon", "sun", "planet", "galaxy", "nebula", "meteor",
	"cactus", "fern", "moss", "tree", "leaf", "sprout", "seed", "berry", "fruit", "veggie",
}

var gemstones = []string{"💎", "🔮", "🌈", "🧿", "⛵️", "🧊", "🏮", "🐚", "🪙", "🧧", "🪬"}

func Gemstone(name string, soul string) string {
	hasher := sha256.New()
	hasher.Write([]byte(name + soul))
	hash := hasher.Sum(nil)
	index := int(binary.BigEndian.Uint64(hash[:8]) % uint64(len(gemstones)))
	return gemstones[index]
}

func GenerateName(id string) string {
	if id == "" {
		return "unnamed-nara"
	}

	hasher := sha256.New()
	hasher.Write([]byte(id))
	hash := hasher.Sum(nil)
	seed := int64(binary.BigEndian.Uint64(hash[:8]))
	r := rand.New(rand.NewSource(seed))

	adj := adjectives[r.Intn(len(adjectives))]
	noun := nouns[r.Intn(len(nouns))]
	suffix := r.Intn(1000)

	return fmt.Sprintf("%s-%s-%03d", adj, noun, suffix)
}

func CollectSoulFragments() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	var macs []string
	for _, iface := range interfaces {
		// Skip loopback and interfaces with no MAC
		if iface.Flags&net.FlagLoopback != 0 || iface.HardwareAddr.String() == "" {
			continue
		}

		// Filter for physical-ish interfaces to stay stable
		name := iface.Name
		isPhysical := strings.HasPrefix(name, "en") || strings.HasPrefix(name, "eth") || strings.HasPrefix(name, "wlan")
		if !isPhysical {
			continue
		}

		macs = append(macs, iface.HardwareAddr.String())
	}

	// Sort to ensure deterministic order regardless of how OS returns them
	sort.Strings(macs)

	return strings.Join(macs, ",")
}

// HashHardware creates a hardware fingerprint hash
func HashHardware(fragments string) []byte {
	return hashBytes([]byte(fragments))
}

func hashBytes(data []byte) []byte {
	hasher := sha256.New()
	hasher.Write(data)
	return hasher.Sum(nil)
}

func IsGenericHostname(hostname string) bool {
	genericNames := []string{"localhost", "raspberrypi", "raspberry", "debian", "ubuntu", "nixos", "nara"}
	for _, name := range genericNames {
		if strings.Contains(strings.ToLower(hostname), name) {
			return true
		}
	}
	return false
}
