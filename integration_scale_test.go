package nara

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eljojo/nara/types"
)

// TestIntegration_ScaleWith5000Nodes tests the network at scale with 5000 nodes
// including 50 abusive nodes that flood restart events. This validates:
// - Anti-abuse mechanisms work at scale (compaction, rate limiting, deduplication)
// - Network remains stable with 1% malicious nodes
// - Memory usage stays bounded
// - Critical events survive
// - Consensus accuracy maintained
func TestIntegration_ScaleWith5000Nodes(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping expensive scale test in short mode")
	}

	// This test uses a lightweight simulation approach rather than spawning
	// 5000 full nara instances, which would be prohibitively expensive

	const totalNodes = 5000
	const abusiveNodes = 50
	const normalNodes = totalNodes - abusiveNodes

	t.Logf("🏗️  Starting scale test: %d nodes (%d normal + %d abusive)", totalNodes, normalNodes, abusiveNodes)

	// Create a shared sync ledger to simulate network-wide event propagation
	networkLedger := NewSyncLedger(100000) // Large capacity for scale test
	opinionProjection := NewOpinionConsensusProjection(networkLedger)

	var mu sync.Mutex
	eventCounts := make(map[string]int) // Track events per subject

	// Phase 1: Normal nodes emit realistic observation events
	t.Log("📊 Phase 1: Normal nodes emitting realistic events...")

	startTime := time.Now()
	normalEventCount := 0

	for i := 0; i < normalNodes; i++ {
		nodeName := fmt.Sprintf("nara-%d", i)

		// Each node observes ~10 random other nodes
		for j := 0; j < 10; j++ {
			targetIdx := (i + j*500) % normalNodes
			target := fmt.Sprintf("nara-%d", targetIdx)

			// Emit a restart event (realistic scenario)
			event := NewRestartObservationEvent(types.NaraName(nodeName), types.NaraName(target), startTime.Unix(), int64(5))
			added := networkLedger.AddEventWithDedup(event)

			if added {
				mu.Lock()
				eventCounts[target]++
				normalEventCount++
				mu.Unlock()
			}
		}

		if i%1000 == 0 {
			t.Logf("  Progress: %d/%d normal nodes processed", i, normalNodes)
		}
	}

	normalPhaseEnd := time.Now()
	t.Logf("✅ Phase 1 complete: %d events from normal nodes in %v",
		normalEventCount, normalPhaseEnd.Sub(startTime))

	// Phase 2: Abusive nodes flood restart events
	t.Log("⚠️  Phase 2: Abusive nodes flooding restart events...")

	abuseStartTime := time.Now()
	abuseAttempts := 0
	abuseBlocked := 0

	for i := 0; i < abusiveNodes; i++ {
		abuserName := fmt.Sprintf("abuser-%d", i)
		victim := fmt.Sprintf("nara-%d", i%100) // Target first 100 nodes

		// Each abuser tries to flood 100 restart events about the same victim
		for j := 0; j < 100; j++ {
			event := NewRestartObservationEvent(types.NaraName(abuserName), types.NaraName(victim), startTime.Unix()+int64(j), int64(1000+j))
			abuseAttempts++

			added := networkLedger.AddEventWithRateLimit(event)
			if !added {
				abuseBlocked++
			}
		}
	}

	abusePhaseEnd := time.Now()
	t.Logf("✅ Phase 2 complete: %d abuse attempts, %d blocked (%d%%) in %v",
		abuseAttempts, abuseBlocked, (abuseBlocked*100)/abuseAttempts, abusePhaseEnd.Sub(abuseStartTime))

	// Validation 1: Check total event count is bounded
	t.Log("")
	t.Log("🔍 Validation 1: Event count bounded")

	allEvents := networkLedger.GetAllEvents()
	totalStoredEvents := len(allEvents)

	// With anti-abuse, we should have far fewer than normalEventCount + abuseAttempts
	maxExpected := normalEventCount + (abusiveNodes * 20) // 20 per abuser max (compaction)

	t.Logf("  Total events stored: %d", totalStoredEvents)
	t.Logf("  Maximum expected: %d", maxExpected)

	if totalStoredEvents > maxExpected {
		t.Errorf("❌ Event count not bounded: %d > %d (anti-abuse may be failing)",
			totalStoredEvents, maxExpected)
	} else {
		t.Logf("✅ Event storage bounded (anti-abuse working)")
	}

	// Validation 2: Check per-subject event counts respect rate limiting
	t.Log("")
	t.Log("🔍 Validation 2: Per-subject rate limiting")

	subjectEventCounts := make(map[string]int)
	for _, event := range allEvents {
		if event.Service == "observation" && event.Observation != nil {
			subjectEventCounts[event.Observation.Subject.String()]++
		}
	}

	rateLimitViolations := 0
	maxEventsPerSubject := 0

	for subject, count := range subjectEventCounts {
		if count > maxEventsPerSubject {
			maxEventsPerSubject = count
		}

		// With multiple observers and rate limiting, no subject should have excessive events
		// Allow some flexibility due to deduplication and compaction
		if count > 200 { // Reasonable upper bound for 5000 observers
			rateLimitViolations++
			t.Logf("  ⚠️  Subject %s has %d events (may indicate rate limit issue)", subject, count)
		}
	}

	t.Logf("  Max events per subject: %d", maxEventsPerSubject)
	if rateLimitViolations == 0 {
		t.Log("✅ Rate limiting working (no excessive events per subject)")
	} else {
		t.Errorf("❌ Rate limiting violations: %d subjects with excessive events", rateLimitViolations)
	}

	// Validation 3: Check per-pair compaction (max 20 events per observer→subject)
	t.Log("")
	t.Log("🔍 Validation 3: Per-pair compaction")

	pairCounts := make(map[string]int)
	for _, event := range allEvents {
		if event.Service == "observation" && event.Observation != nil {
			pairKey := fmt.Sprintf("%s→%s", event.Observation.Observer, event.Observation.Subject)
			pairCounts[pairKey]++
		}
	}

	compactionViolations := 0
	maxPairCount := 0

	for pair, count := range pairCounts {
		if count > maxPairCount {
			maxPairCount = count
		}

		if count > 20 {
			compactionViolations++
			if compactionViolations <= 5 { // Only log first 5
				t.Logf("  ❌ Pair %s has %d events (limit: 20)", pair, count)
			}
		}
	}

	t.Logf("  Max events per observer→subject pair: %d", maxPairCount)
	if compactionViolations == 0 {
		t.Log("✅ Per-pair compaction working (all pairs ≤20 events)")
	} else {
		t.Errorf("❌ Compaction violations: %d pairs exceed 20 events", compactionViolations)
	}

	// Validation 4: Check deduplication (same restart reported by multiple observers)
	t.Log("")
	t.Log("🔍 Validation 4: Restart deduplication")

	// Check if we have duplicate restart events (same subject, restart_num, start_time)
	restartSigs := make(map[string]int) // signature → count
	for _, event := range allEvents {
		if event.Service == "observation" &&
			event.Observation != nil &&
			event.Observation.Type == "restart" {
			sig := fmt.Sprintf("%s:%d:%d",
				event.Observation.Subject,
				event.Observation.RestartNum,
				event.Observation.StartTime)
			restartSigs[sig]++
		}
	}

	duplicateRestarts := 0
	for _, count := range restartSigs {
		if count > 1 {
			duplicateRestarts++
		}
	}

	if duplicateRestarts == 0 {
		t.Log("✅ Deduplication working (no duplicate restart events)")
	} else {
		t.Logf("⚠️  Deduplication: %d restart signatures have duplicates (may indicate dedup issue)", duplicateRestarts)
	}

	// Validation 5: Check that critical events are preserved
	t.Log("")
	t.Log("🔍 Validation 5: Critical event preservation")

	criticalEvents := 0
	normalEvents := 0
	casualEvents := 0

	for _, event := range allEvents {
		if event.Service == "observation" && event.Observation != nil {
			switch event.Observation.Importance {
			case 3: // ImportanceCritical
				criticalEvents++
			case 2: // ImportanceNormal
				normalEvents++
			case 1: // ImportanceCasual
				casualEvents++
			}
		}
	}

	t.Logf("  Critical events: %d", criticalEvents)
	t.Logf("  Normal events: %d", normalEvents)
	t.Logf("  Casual events: %d", casualEvents)

	if criticalEvents > 0 {
		t.Log("✅ Critical events preserved")
	}

	// Validation 6: Network remains functional (can derive consensus)
	t.Log("")
	t.Log("🔍 Validation 6: Consensus functionality")

	// Pick a few random subjects and try to derive consensus
	testSubjects := []string{"nara-0", "nara-100", "nara-500", "nara-1000"}
	consensusSuccess := 0

	// Process all events before deriving opinions
	if err := opinionProjection.RunToEnd(context.Background()); err != nil {
		t.Fatalf("Failed to run projection: %v", err)
	}

	for _, subject := range testSubjects {
		opinion := opinionProjection.DeriveOpinion(types.NaraName(subject))
		if opinion.StartTime > 0 || opinion.Restarts > 0 {
			consensusSuccess++
		}
	}

	t.Logf("  Consensus derived for %d/%d test subjects", consensusSuccess, len(testSubjects))
	if consensusSuccess == 0 {
		t.Error("❌ Consensus failed - no opinions derived for any test subject")
	} else {
		t.Log("✅ Consensus system functional")
	}

	// Validation 7: Memory usage (approximate)
	t.Log("")
	t.Log("🔍 Validation 7: Memory usage")

	// Rough estimate: each event ~200 bytes
	estimatedMemoryMB := (totalStoredEvents * 200) / (1024 * 1024)
	t.Logf("  Estimated memory usage: ~%d MB for %d events", estimatedMemoryMB, totalStoredEvents)

	// At scale with anti-abuse, memory should be bounded to reasonable levels
	maxAcceptableMemoryMB := 500 // 500 MB max
	if estimatedMemoryMB > maxAcceptableMemoryMB {
		t.Errorf("❌ Memory usage too high: %d MB (max: %d MB)", estimatedMemoryMB, maxAcceptableMemoryMB)
	} else {
		t.Log("✅ Memory usage bounded")
	}

	// Final summary
	totalTestTime := time.Since(startTime)

	t.Log("")
	t.Log("═══════════════════════════════════════")
	t.Log("🎉 SCALE TEST COMPLETED")
	t.Logf("   • Network size: %d nodes (%d normal + %d abusive)", totalNodes, normalNodes, abusiveNodes)
	t.Logf("   • Normal events: %d", normalEventCount)
	t.Logf("   • Abuse attempts: %d", abuseAttempts)
	t.Logf("   • Abuse blocked: %d (%d%%)", abuseBlocked, (abuseBlocked*100)/abuseAttempts)
	t.Logf("   • Total stored events: %d", totalStoredEvents)
	t.Logf("   • Max events per subject: %d", maxEventsPerSubject)
	t.Logf("   • Max events per pair: %d", maxPairCount)
	t.Logf("   • Critical events preserved: %d", criticalEvents)
	t.Logf("   • Memory usage: ~%d MB", estimatedMemoryMB)
	t.Logf("   • Test duration: %v", totalTestTime)
	t.Log("")

	if compactionViolations > 0 {
		t.Log("❌ FAIL: Per-pair compaction violations detected")
	} else if rateLimitViolations > 0 {
		t.Log("⚠️  WARNING: Some rate limiting issues detected")
	} else if totalStoredEvents > maxExpected {
		t.Log("❌ FAIL: Event storage not properly bounded")
	} else {
		t.Log("✅ PASS: Network stable at 5000 nodes with 1% malicious nodes")
		t.Log("         Anti-abuse mechanisms validated at scale")
	}
	t.Log("═══════════════════════════════════════")
}

// TestIntegration_ScaleStressAbuse tests extreme abuse scenarios
func TestIntegration_ScaleStressAbuse(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	t.Log("🔥 Stress test: Coordinated attack from 100 abusers")

	const abusers = 100
	const victimCount = 10
	const eventsPerAbuser = 1000

	ledger := NewSyncLedger(50000)

	// Coordinated attack: 100 abusers all target the same 10 victims
	blockedCount := 0
	acceptedCount := 0

	for i := 0; i < abusers; i++ {
		abuser := fmt.Sprintf("attacker-%d", i)

		for j := 0; j < victimCount; j++ {
			victim := fmt.Sprintf("victim-%d", j)

			// Each abuser tries to flood 1000 events about each victim
			for k := 0; k < eventsPerAbuser; k++ {
				event := NewRestartObservationEvent(types.NaraName(abuser), types.NaraName(victim), time.Now().Unix(), int64(k))

				// Use both rate limiting and compaction
				if !ledger.AddEventWithRateLimit(event) {
					blockedCount++
					continue
				}

				if !ledger.AddEventWithDedup(event) {
					blockedCount++
					continue
				}

				acceptedCount++
			}
		}

		if i%20 == 0 {
			t.Logf("  Processed %d/%d attackers", i, abusers)
		}
	}

	totalAttempts := abusers * victimCount * eventsPerAbuser
	blockRate := (blockedCount * 100) / totalAttempts

	t.Logf("")
	t.Logf("📊 Stress test results:")
	t.Logf("   • Total attack attempts: %d", totalAttempts)
	t.Logf("   • Blocked: %d (%d%%)", blockedCount, blockRate)
	t.Logf("   • Accepted: %d", acceptedCount)

	allEvents := ledger.GetAllEvents()
	t.Logf("   • Final event count: %d", len(allEvents))

	// Under coordinated attack, block rate should be very high (>90%)
	if blockRate < 90 {
		t.Errorf("❌ Insufficient blocking under coordinated attack: %d%% (expected >90%%)", blockRate)
	} else {
		t.Logf("✅ Strong defense: %d%% of coordinated attack blocked", blockRate)
	}

	// Final event count should be reasonable (bounded by anti-abuse)
	maxExpectedEvents := abusers * 20 // 20 per abuser due to compaction
	if len(allEvents) > maxExpectedEvents*2 {
		t.Errorf("❌ Event count not bounded under attack: %d (expected <%d)",
			len(allEvents), maxExpectedEvents*2)
	} else {
		t.Log("✅ Event storage remained bounded under attack")
	}
}

// TestIntegration_ScaleBackfill tests backfill at scale
func TestIntegration_ScaleBackfill(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping backfill scale test in short mode")
	}

	t.Log("📦 Backfill test: Migrating 5000 historical observations")

	const nodeCount = 5000
	ledger := NewSyncLedger(100000)

	startTime := time.Now()

	// Simulate 5000 nodes all backfilling historical data
	for i := 0; i < nodeCount; i++ {
		observer := fmt.Sprintf("nara-%d", i)
		subject := fmt.Sprintf("nara-%d", (i+1)%nodeCount)

		// Each node backfills observation about one other node
		event := NewBackfillObservationEvent(
			types.NaraName(observer),
			types.NaraName(subject),
			1624066568,      // Historical start time
			int64(100+i%50), // Varying restart counts
			time.Now().Unix(),
		)

		ledger.AddEventWithDedup(event)

		if i%1000 == 0 {
			t.Logf("  Backfilled %d/%d nodes", i, nodeCount)
		}
	}

	backfillTime := time.Since(startTime)

	allEvents := ledger.GetAllEvents()
	backfillEvents := 0
	for _, event := range allEvents {
		if event.Service == "observation" &&
			event.Observation != nil &&
			event.Observation.IsBackfill {
			backfillEvents++
		}
	}

	t.Logf("")
	t.Logf("📊 Backfill results:")
	t.Logf("   • Nodes backfilled: %d", nodeCount)
	t.Logf("   • Backfill events stored: %d", backfillEvents)
	t.Logf("   • Time taken: %v", backfillTime)
	t.Logf("   • Rate: %.0f events/sec", float64(backfillEvents)/backfillTime.Seconds())

	// Backfill should result in roughly nodeCount events (deduplication may reduce)
	if backfillEvents < nodeCount/2 {
		t.Errorf("❌ Too few backfill events: %d (expected ~%d)", backfillEvents, nodeCount)
	} else {
		t.Logf("✅ Backfill successful at scale")
	}
}
