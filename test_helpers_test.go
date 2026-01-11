package nara

import (
	"crypto/sha256"
	"os"
	"testing"

	"github.com/sirupsen/logrus"
)

// testSoul generates a valid soul for testing given a name.
// This ensures all test naras have valid keypairs for signing.
func testSoul(name string) string {
	hw := hashTestBytes([]byte("test-hardware-" + name))
	soul := NativeSoulCustom(hw, name)
	return FormatSoul(soul)
}

func hashTestBytes(b []byte) []byte {
	h := sha256.Sum256(b)
	return h[:]
}

// TestMain runs before all tests to set up global test configuration
func TestMain(m *testing.M) {
	OpinionRepeatOverride = 1
	OpinionIntervalOverride = 0

	// Set default log level to warnings and above for cleaner test output
	// This still shows warnings and errors, but suppresses info/debug logs
	// Individual tests can override this with logrus.SetLevel(logrus.DebugLevel)
	logrus.SetLevel(logrus.WarnLevel)

	// Run tests
	exitCode := m.Run()

	os.Exit(exitCode)
}

// testLocalNara creates a LocalNara for testing with a valid identity bonded to the name.
func testLocalNara(name string) *LocalNara {
	identity := testIdentity(name)
	profile := DefaultMemoryProfile()
	ln, err := NewLocalNara(identity, "host", "user", "pass", -1, profile)
	if err != nil {
		panic(err)
	}
	return ln
}

// testLocalNaraWithParams creates a LocalNara for testing with specific chattiness and ledger capacity.
func testLocalNaraWithParams(name string, chattiness int, ledgerCapacity int) *LocalNara {
	identity := testIdentity(name)
	profile := DefaultMemoryProfile()
	if ledgerCapacity > 0 {
		profile.Mode = MemoryModeCustom
		profile.MaxEvents = ledgerCapacity
	}
	ln, err := NewLocalNara(identity, "", "", "", chattiness, profile)
	if err != nil {
		panic(err)
	}
	return ln
}

// testLocalNaraWithSoul creates a LocalNara for testing with a specific soul string.
func testLocalNaraWithSoul(name string, soul string) *LocalNara {
	parsed, _ := ParseSoul(soul)
	id, _ := ComputeNaraID(soul, name)
	identity := IdentityResult{
		Name:        name,
		Soul:        parsed,
		ID:          id,
		IsValidBond: true,
		IsNative:    true,
	}
	profile := DefaultMemoryProfile()
	ln, err := NewLocalNara(identity, "host", "user", "pass", -1, profile)
	if err != nil {
		panic(err)
	}
	return ln
}

func testIdentity(name string) IdentityResult {
	soulStr := testSoul(name)
	parsed, _ := ParseSoul(soulStr)
	id, _ := ComputeNaraID(soulStr, name)
	return IdentityResult{
		Name:        name,
		Soul:        parsed,
		ID:          id,
		IsValidBond: true,
		IsNative:    true,
	}
}

// testLocalNaraWithSoulAndParams creates a LocalNara for testing with a specific soul and parameters.
func testLocalNaraWithSoulAndParams(name string, soul string, chattiness int, ledgerCapacity int) *LocalNara {
	parsed, _ := ParseSoul(soul)
	id, _ := ComputeNaraID(soul, name)
	identity := IdentityResult{
		Name:        name,
		Soul:        parsed,
		ID:          id,
		IsValidBond: true,
		IsNative:    true,
	}
	profile := DefaultMemoryProfile()
	if ledgerCapacity > 0 {
		profile.Mode = MemoryModeCustom
		profile.MaxEvents = ledgerCapacity
	}
	ln, err := NewLocalNara(identity, "", "", "", chattiness, profile)
	if err != nil {
		panic(err)
	}
	return ln
}
