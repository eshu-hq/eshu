package status

import "time"

// ProducerActivitySnapshot captures recent fact-producing generation movement.
// It lets status distinguish an idle reducer gap from a still-active producer.
type ProducerActivitySnapshot struct {
	HasActiveOrPendingGeneration bool
	LatestGenerationAge          time.Duration
}

func normalizeProducerActivitySnapshot(snapshot ProducerActivitySnapshot) ProducerActivitySnapshot {
	return ProducerActivitySnapshot{
		HasActiveOrPendingGeneration: snapshot.HasActiveOrPendingGeneration,
		LatestGenerationAge:          nonNegativeDuration(snapshot.LatestGenerationAge),
	}
}
