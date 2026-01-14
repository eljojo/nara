package nara

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAura(t *testing.T) {
	// Test different personality types generate appropriate colors
	testCases := []struct {
		name          string
		soul          string
		sociability   int
		agreeableness int
		chill         int
		description   string
	}{
		{
			name:          "neon-lover",
			soul:          "test-soul-neon",
			sociability:   80,
			agreeableness: 50,
			chill:         20,
			description:   "high sociability, low chill = neon colors",
		},
		{
			name:          "pastel-sweetheart",
			soul:          "test-soul-pastel",
			sociability:   50,
			agreeableness: 85,
			chill:         60,
			description:   "high agreeableness = pastel colors",
		},
		{
			name:          "earth-tone",
			soul:          "test-soul-earth",
			sociability:   40,
			agreeableness: 50,
			chill:         85,
			description:   "very chill = earth tones",
		},
		{
			name:          "bold-contrast",
			soul:          "test-soul-bold",
			sociability:   70,
			agreeableness: 20,
			chill:         40,
			description:   "disagreeable but social = bold contrasts",
		},
		{
			name:          "muted-withdrawn",
			soul:          "test-soul-muted",
			sociability:   20,
			agreeableness: 40,
			chill:         70,
			description:   "low sociability, high chill = muted/withdrawn",
		},
		{
			name:          "pantone-designer",
			soul:          "test-soul-pantone",
			sociability:   50,
			agreeableness: 50,
			chill:         50,
			description:   "balanced = pantone designer colors",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Use testSoul to generate valid Base58 soul for ID computation
			ln := testLocalNaraWithSoul(t, tc.name, testSoul(tc.soul))
			ln.Me.Status.Personality.Sociability = tc.sociability
			ln.Me.Status.Personality.Agreeableness = tc.agreeableness
			ln.Me.Status.Personality.Chill = tc.chill

			aura := ln.computeAura()

			// Check primary is a valid hex color
			if len(aura.Primary) != 7 || aura.Primary[0] != '#' {
				t.Errorf("Invalid primary aura format: %s (expected #RRGGBB)", aura.Primary)
			}

			// Check secondary is a valid hex color
			if len(aura.Secondary) != 7 || aura.Secondary[0] != '#' {
				t.Errorf("Invalid secondary aura format: %s (expected #RRGGBB)", aura.Secondary)
			}

			t.Logf("%s: primary=%s secondary=%s - %s", tc.name, aura.Primary, aura.Secondary, tc.description)

			// Test determinism - same soul + personality = same colors
			aura2 := ln.computeAura()
			if aura.Primary != aura2.Primary || aura.Secondary != aura2.Secondary {
				t.Errorf("Aura should be deterministic: got (%s,%s) then (%s,%s)",
					aura.Primary, aura.Secondary, aura2.Primary, aura2.Secondary)
			}
		})
	}
}

func TestAuraDeterminism(t *testing.T) {
	// Same soul should always produce same color
	ln := testLocalNaraWithParams(t, "test", 50, 1000)
	ln.Me.Status.Personality.Sociability = 60
	ln.Me.Status.Personality.Agreeableness = 60
	ln.Me.Status.Personality.Chill = 60

	aura1 := ln.computeAura()
	aura2 := ln.computeAura()
	aura3 := ln.computeAura()

	if aura1.Primary != aura2.Primary || aura2.Primary != aura3.Primary {
		t.Errorf("Primary aura not deterministic: %s, %s, %s", aura1.Primary, aura2.Primary, aura3.Primary)
	}
	if aura1.Secondary != aura2.Secondary || aura2.Secondary != aura3.Secondary {
		t.Errorf("Secondary aura not deterministic: %s, %s, %s", aura1.Secondary, aura2.Secondary, aura3.Secondary)
	}

	// Different soul should produce different color (usually)
	ln2 := testLocalNaraWithParams(t, "test2", 50, 1000)
	ln2.Me.Status.Personality.Sociability = 60
	ln2.Me.Status.Personality.Agreeableness = 60
	ln2.Me.Status.Personality.Chill = 60

	aura4 := ln2.computeAura()

	// With high probability, different souls give different colors
	t.Logf("Soul 1: primary=%s secondary=%s, Soul 2: primary=%s secondary=%s",
		aura1.Primary, aura1.Secondary, aura4.Primary, aura4.Secondary)
}

func TestAuraInStatus(t *testing.T) {
	// Test that aura is set during nara initialization
	ln := testLocalNaraWithParams(t, "test-aura", 50, 1000)
	// Check that aura is set immediately after initialization
	if ln.Me.Status.Aura.Primary == "" {
		t.Error("Aura.Primary should be set during nara initialization, but it's empty")
	}
	if ln.Me.Status.Aura.Secondary == "" {
		t.Error("Aura.Secondary should be set during nara initialization, but it's empty")
	}

	// Check they're valid hex colors
	if len(ln.Me.Status.Aura.Primary) != 7 || ln.Me.Status.Aura.Primary[0] != '#' {
		t.Errorf("Invalid primary aura format: %s (expected #RRGGBB)", ln.Me.Status.Aura.Primary)
	}
	if len(ln.Me.Status.Aura.Secondary) != 7 || ln.Me.Status.Aura.Secondary[0] != '#' {
		t.Errorf("Invalid secondary aura format: %s (expected #RRGGBB)", ln.Me.Status.Aura.Secondary)
	}

	t.Logf("Nara %s has aura: primary=%s secondary=%s", ln.Me.Name, ln.Me.Status.Aura.Primary, ln.Me.Status.Aura.Secondary)
}

func TestAuraInSetValuesFrom(t *testing.T) {
	// Test that aura is copied when receiving status from other naras
	ln1 := testLocalNaraWithParams(t, "nara1", 50, 1000)
	ln2 := testLocalNaraWithParams(t, "nara2", 50, 1000)
	// nara1 should have an aura
	if ln1.Me.Status.Aura.Primary == "" {
		t.Fatal("nara1 should have an aura primary")
	}

	originalAura := ln1.Me.Status.Aura
	t.Logf("nara1 original aura: primary=%s secondary=%s", originalAura.Primary, originalAura.Secondary)

	// Create a nara in nara2's neighbourhood representing nara1
	nara1InNara2 := NewNara("nara1")
	ln2.Network.importNara(nara1InNara2)

	// Fetch imported nara back (to be sure we have the reference in the map)
	nara1InNara2Ref := ln2.Network.Neighbourhood["nara1"]

	// Initially should not have an aura
	if nara1InNara2Ref.Status.Aura.Primary != "" {
		t.Error("Imported nara should not have an aura before setValuesFrom")
	}

	// Copy values from nara1 to the representation in nara2's neighbourhood
	nara1InNara2Ref.setValuesFrom(ln1.Me)

	// Now it should have the aura
	if nara1InNara2Ref.Status.Aura.Primary == "" {
		t.Error("Aura.Primary should be copied via setValuesFrom, but it's empty")
	}
	if nara1InNara2Ref.Status.Aura.Secondary == "" {
		t.Error("Aura.Secondary should be copied via setValuesFrom, but it's empty")
	}

	if nara1InNara2Ref.Status.Aura.Primary != originalAura.Primary {
		t.Errorf("Primary not copied correctly: got %s, want %s",
			nara1InNara2Ref.Status.Aura.Primary, originalAura.Primary)
	}
	if nara1InNara2Ref.Status.Aura.Secondary != originalAura.Secondary {
		t.Errorf("Secondary not copied correctly: got %s, want %s",
			nara1InNara2Ref.Status.Aura.Secondary, originalAura.Secondary)
	}

	t.Logf("✅ Aura successfully copied: primary=%s secondary=%s",
		nara1InNara2Ref.Status.Aura.Primary, nara1InNara2Ref.Status.Aura.Secondary)
}

func TestAuraInMaintenance(t *testing.T) {
	// Test that observationMaintenanceOnce updates the aura
	ln := testLocalNaraWithParams(t, "test-maintenance", 50, 1000)
	// Set initial personality to get a specific palette (neon lovers)
	ln.Me.Status.Personality.Sociability = 80
	ln.Me.Status.Personality.Agreeableness = 50
	ln.Me.Status.Personality.Chill = 20

	// Manually set an incorrect aura
	ln.Me.mu.Lock()
	ln.Me.Status.Aura = Aura{Primary: "#000000", Secondary: "#000000"}
	ln.Me.mu.Unlock()

	t.Logf("Before maintenance: primary=%s (should be wrong)", ln.Me.Status.Aura.Primary)

	// Run maintenance which should recalculate and update aura
	ln.Network.observationMaintenanceOnce()

	// Check that aura was updated
	ln.Me.mu.Lock()
	updatedAura := ln.Me.Status.Aura
	ln.Me.mu.Unlock()

	if updatedAura.Primary == "#000000" {
		t.Error("Aura.Primary should have been updated by maintenance, but it's still #000000")
	}
	if updatedAura.Secondary == "#000000" {
		t.Error("Aura.Secondary should have been updated by maintenance, but it's still #000000")
	}

	// Verify they're valid hex colors
	if len(updatedAura.Primary) != 7 || updatedAura.Primary[0] != '#' {
		t.Errorf("Invalid primary aura format after maintenance: %s", updatedAura.Primary)
	}
	if len(updatedAura.Secondary) != 7 || updatedAura.Secondary[0] != '#' {
		t.Errorf("Invalid secondary aura format after maintenance: %s", updatedAura.Secondary)
	}

	// Verify it matches what computeAura() would return for this personality
	expectedAura := ln.computeAura()
	if updatedAura.Primary != expectedAura.Primary {
		t.Errorf("Maintenance set primary to %s, but computeAura() returns %s", updatedAura.Primary, expectedAura.Primary)
	}
	if updatedAura.Secondary != expectedAura.Secondary {
		t.Errorf("Maintenance set secondary to %s, but computeAura() returns %s", updatedAura.Secondary, expectedAura.Secondary)
	}

	t.Logf("✅ Maintenance updated aura: primary=%s secondary=%s (personality: sociability=%d, agreeableness=%d, chill=%d)",
		updatedAura.Primary, updatedAura.Secondary, ln.Me.Status.Personality.Sociability,
		ln.Me.Status.Personality.Agreeableness, ln.Me.Status.Personality.Chill)
}

func TestAuraInHTTPApi(t *testing.T) {
	// Test that aura appears in HTTP API responses
	ln := testLocalNaraWithParams(t, "test-http", 50, 1000)
	// Set a specific personality to get a predictable aura
	ln.Me.Status.Personality.Sociability = 75
	ln.Me.Status.Personality.Agreeableness = 55
	ln.Me.Status.Personality.Chill = 25

	// Recalculate aura after changing personality
	expectedAura := ln.computeAura()
	ln.Me.mu.Lock()
	ln.Me.Status.Aura = expectedAura
	ln.Me.mu.Unlock()

	t.Logf("Expected aura in API: primary=%s secondary=%s", expectedAura.Primary, expectedAura.Secondary)

	// Create HTTP request for /api.json
	req, err := http.NewRequest("GET", "/api.json", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(ln.Network.httpApiJsonHandler)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	// Parse response
	var response map[string]interface{}
	err = json.Unmarshal(rr.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to parse JSON response: %v\nBody: %s", err, rr.Body.String())
	}

	// Check naras array
	naras, ok := response["naras"].([]interface{})
	if !ok {
		t.Fatal("Expected 'naras' array in response")
	}

	if len(naras) == 0 {
		t.Fatal("Expected at least 1 nara in response")
	}

	// Find our nara
	var foundNara map[string]interface{}
	for _, n := range naras {
		nara := n.(map[string]interface{})
		if nara["Name"] == "test-http" {
			foundNara = nara
			break
		}
	}

	if foundNara == nil {
		t.Fatal("Could not find test-http nara in response")
	}

	// The /api.json endpoint returns fields flattened at the top level
	// Aura should be an object with primary and secondary
	auraObj, ok := foundNara["Aura"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected 'Aura' object in nara, got: %+v", foundNara)
	}

	primary, _ := auraObj["primary"].(string)
	secondary, _ := auraObj["secondary"].(string)

	if primary == "" {
		t.Error("Aura.primary field is empty in API response")
	}
	if secondary == "" {
		t.Error("Aura.secondary field is empty in API response")
	}

	if primary != expectedAura.Primary {
		t.Errorf("Aura.primary mismatch: API returned %s, expected %s", primary, expectedAura.Primary)
	}
	if secondary != expectedAura.Secondary {
		t.Errorf("Aura.secondary mismatch: API returned %s, expected %s", secondary, expectedAura.Secondary)
	}

	// Verify they're valid hex colors
	if len(primary) != 7 || primary[0] != '#' {
		t.Errorf("Invalid primary format in API: %s (expected #RRGGBB)", primary)
	}
	if len(secondary) != 7 || secondary[0] != '#' {
		t.Errorf("Invalid secondary format in API: %s (expected #RRGGBB)", secondary)
	}

	t.Logf("✅ Aura appears correctly in HTTP API: primary=%s secondary=%s", primary, secondary)
}
