package knowledge

import "strings"

// Режимы второго этапа обработки кандидатов после векторного поиска.
const (
	RerankModeThreshold = "threshold"
	RerankModeNone      = "none"
)

// Reranker — второй этап retrieval: фильтрация и/или переранжирование
// кандидатов, найденных по векторной близости.
type Reranker interface {
	Rerank(query string, candidates []ScoredChunk) ([]ScoredChunk, error)
}

// NoopReranker оставляет кандидатов без изменений.
type NoopReranker struct{}

// Rerank возвращает кандидатов как есть.
func (NoopReranker) Rerank(_ string, candidates []ScoredChunk) ([]ScoredChunk, error) {
	return candidates, nil
}

// ThresholdReranker отсекает кандидатов с близостью (cosine) ниже порога.
// Отрицательный порог отключает фильтрацию.
type ThresholdReranker struct {
	Min float64
}

// Rerank сохраняет кандидатов с Score >= Min, не меняя их порядок.
func (r ThresholdReranker) Rerank(_ string, candidates []ScoredChunk) ([]ScoredChunk, error) {
	if r.Min < 0 {
		return candidates, nil
	}
	out := make([]ScoredChunk, 0, len(candidates))
	for _, c := range candidates {
		if c.Score >= r.Min {
			out = append(out, c)
		}
	}
	return out, nil
}

// NewReranker выбирает реализацию второго этапа по режиму.
// Неизвестный или пустой режим трактуется как threshold.
func NewReranker(mode string, threshold float64) Reranker {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case RerankModeNone:
		return NoopReranker{}
	default:
		return ThresholdReranker{Min: threshold}
	}
}
