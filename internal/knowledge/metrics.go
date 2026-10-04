package knowledge

import "sort"

// QueryResult — результат одного тест-запроса в бенчмарке.
type QueryResult struct {
	Hit       bool
	FirstRank int // 1-based позиция первого релевантного результата; 0 если нет
	Matches   int // число релевантных результатов в top-K
}

// Evaluate считает Recall@K, Precision@K и MRR по результатам запросов.
func Evaluate(results []QueryResult, k int) (recall, precision, mrr float64) {
	if len(results) == 0 || k <= 0 {
		return 0, 0, 0
	}

	var hits, matchSum, reciprocalSum float64
	for _, r := range results {
		if r.Hit {
			hits++
		}
		matchSum += float64(r.Matches)
		if r.FirstRank > 0 {
			reciprocalSum += 1.0 / float64(r.FirstRank)
		}
	}

	n := float64(len(results))
	return hits / n, matchSum / (n * float64(k)), reciprocalSum / n
}

// ScoredChunk — чанк с оценкой близости.
type ScoredChunk struct {
	StoredChunk
	Score float64
}

// SearchTopK возвращает top-K чанков по косинусной близости к запросу.
// Векторы предполагаются нормализованными, поэтому близость — скалярное произведение.
func SearchTopK(chunks []StoredChunk, query []float32, k int) []ScoredChunk {
	scored := make([]ScoredChunk, 0, len(chunks))
	for _, c := range chunks {
		if len(c.Vector) != len(query) {
			continue
		}
		scored = append(scored, ScoredChunk{StoredChunk: c, Score: dot(c.Vector, query)})
	}

	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	if k > 0 && len(scored) > k {
		scored = scored[:k]
	}
	return scored
}

func dot(a, b []float32) float64 {
	var sum float64
	for i := range a {
		sum += float64(a[i]) * float64(b[i])
	}
	return sum
}
