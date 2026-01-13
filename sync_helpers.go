package nara

func socialEventIsMeaningful(payload *SocialEventPayload, personality NaraPersonality) bool {
	if payload == nil {
		return false
	}

	// High chill naras don't bother with random jabs
	if personality.Chill > 70 && payload.Reason == ReasonRandom {
		return false
	}

	// Handle observation events specially
	if payload.Type == "observation" {
		// Everyone keeps journey-timeout (reliability matters!)
		if payload.Reason == ReasonJourneyTimeout {
			return true
		}

		// Very chill naras skip routine online/offline
		if personality.Chill > 85 {
			if payload.Reason == ReasonOnline || payload.Reason == ReasonOffline {
				return false
			}
		}

		// Low sociability naras skip journey-pass/complete
		if personality.Sociability < 30 {
			if payload.Reason == ReasonJourneyPass || payload.Reason == ReasonJourneyComplete {
				return false
			}
		}

		return true
	}

	// Very chill naras only care about significant events (for non-observation types)
	if personality.Chill > 85 {
		// Only store comebacks and high-restarts (significant events)
		if payload.Reason != ReasonComeback && payload.Reason != ReasonHighRestarts {
			return false
		}
	}

	// Highly agreeable naras don't like storing negative drama
	if personality.Agreeableness > 80 && payload.Reason == ReasonTrendAbandon {
		return false // "who am I to judge their choices"
	}

	// Low sociability naras are less interested in others' drama
	if personality.Sociability < 20 {
		// Only store if it seems important (not random)
		if payload.Reason == ReasonRandom {
			return false
		}
	}

	return true
}

// AddSocialEventFiltered adds a social event if personality finds it meaningful
