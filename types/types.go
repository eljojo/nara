package types

// NaraName is a type-safe wrapper for nara names
type NaraName string

// NaraID is a type-safe wrapper for nara identifiers (public key hashes)
type NaraID string

// String converts NaraName to string
func (n NaraName) String() string {
	return string(n)
}

// String converts NaraID to string
func (n NaraID) String() string {
	return string(n)
}
