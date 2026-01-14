package nara

// ------------------------------------------------------------------
// 🙏 A note to whoever is reading this code:
//
// The MQTT credentials below are lightly obfuscated (XOR) - not for
// real security, just to keep them out of `strings` and casual grep.
//
// These credentials are shared with the nara community for a fun,
// collaborative project. We're trusting you to be a good neighbor.
// Please don't abuse them, share them publicly, or do anything that
// would ruin the fun for everyone else.
//
// Be kind. 🌸
// ------------------------------------------------------------------
//
// Default credentials for MQTT and Headscale connections.

var credKey = []byte("nara")

// XOR-obfuscated default credentials (decoded at runtime)
// To encode new values: for each byte, XOR with credKey[i % len(credKey)]
var defaultMQTTUserEnc = []byte{6, 4, 30, 13, 1, 76, 17, 20, 28, 8, 29, 20, 29, 76, 28, 0, 28, 0, 95, 7, 28, 8, 23, 15, 10}
var defaultMQTTPassEnc = []byte{30, 13, 23, 0, 29, 4, 95, 3, 11, 76, 25, 8, 0, 5, 95, 21, 1, 76, 29, 20, 28, 76, 30, 8, 26, 21, 30, 4, 67, 21, 19, 12, 15, 6, 29, 21, 13, 9, 27, 18}
var defaultHeadscaleURLEnc = []byte{6, 21, 6, 17, 29, 91, 93, 78, 24, 17, 28, 79, 0, 0, 0, 0, 64, 15, 23, 21, 25, 14, 0, 10}
var defaultHeadscaleKeyEnc = []byte{12, 3, 69, 3, 91, 87, 20, 4, 94, 4, 71, 0, 92, 86, 68, 81, 94, 87, 68, 88, 93, 5, 68, 3, 93, 2, 71, 3, 11, 87, 20, 0, 87, 81, 75, 88, 91, 86, 65, 0, 12, 5, 75, 86, 8, 84, 19, 88}

func deobfuscate(enc []byte) string {
	result := make([]byte, len(enc))
	for i, b := range enc {
		result[i] = b ^ credKey[i%len(credKey)]
	}
	return string(result)
}

// DefaultMQTTHost returns the default MQTT broker host
func DefaultMQTTHost() string {
	return "tls://mqtt.nara.network:8883"
}

// DefaultMQTTUser returns the default MQTT username
func DefaultMQTTUser() string {
	return deobfuscate(defaultMQTTUserEnc)
}

// DefaultMQTTPass returns the default MQTT password
func DefaultMQTTPass() string {
	return deobfuscate(defaultMQTTPassEnc)
}

// DefaultHeadscaleURL returns the default Headscale control server URL
func DefaultHeadscaleURL() string {
	return deobfuscate(defaultHeadscaleURLEnc)
}

// DefaultHeadscaleAuthKey returns the default Headscale auth key
func DefaultHeadscaleAuthKey() string {
	return deobfuscate(defaultHeadscaleKeyEnc)
}
