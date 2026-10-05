package knowledge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"path"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"ai-chat/internal/chunk"
	"ai-chat/internal/embed"
)

// Значения второго этапа retrieval по умолчанию.
const (
	defaultCandidates      = 20
	defaultTopK            = 5
	defaultRerankThreshold = 0.35
)

// Config — параметры сервиса базы знаний.
type Config struct {
	ChunkSize       int
	ChunkOverlap    int
	ChunkMinSize    int
	TopK            int
	Candidates      int
	RerankMode      string
	RerankThreshold float64
	EmbedModel      string
}

// RetrieveOptions — параметры одного retrieval-запроса.
// Нулевые поля подставляются из Config.
type RetrieveOptions struct {
	Strategy   string
	Query      string
	Candidates int
	TopK       int
	Mode       string
	Threshold  *float64
}

// retrieveParams — разрешённые параметры второго этапа.
type retrieveParams struct {
	candidates int
	topK       int
	mode       string
	threshold  float64
}

// QueryGenerator генерирует тест-вопрос по тексту чанка (обычно через chat-LLM).
type QueryGenerator func(ctx context.Context, chunkText string) (string, error)

// Metrics — сводные метрики проекта.
type Metrics struct {
	Structural []Structural   `json:"structural"`
	IndexRuns  []IndexRun     `json:"index_runs"`
	Benchmark  []BenchmarkRun `json:"benchmark"`
}

// Service — индексация, поиск и оценка стратегий chunking.
type Service struct {
	store     *Store
	embedder  embed.Client
	fixed     *chunk.FixedChunker
	structure *chunk.StructureChunker
	cfg       Config
	queryGen  QueryGenerator
	mu        sync.Mutex
}

// NewService создаёт сервис базы знаний.
func NewService(store *Store, embedder embed.Client, cfg Config, queryGen QueryGenerator) *Service {
	if cfg.TopK <= 0 {
		cfg.TopK = defaultTopK
	}
	if cfg.Candidates <= 0 {
		cfg.Candidates = defaultCandidates
	}
	if cfg.Candidates < cfg.TopK {
		cfg.Candidates = cfg.TopK
	}
	if strings.TrimSpace(cfg.RerankMode) == "" {
		cfg.RerankMode = RerankModeThreshold
	}
	return &Service{
		store:     store,
		embedder:  embedder,
		fixed:     chunk.NewFixedChunker(cfg.ChunkSize, cfg.ChunkOverlap),
		structure: chunk.NewStructureChunker(cfg.ChunkSize, cfg.ChunkMinSize, cfg.ChunkOverlap),
		cfg:       cfg,
		queryGen:  queryGen,
	}
}

// UploadFile сохраняет текстовый файл без индексации.
func (s *Service) UploadFile(ctx context.Context, projectID, filePath, content string) (File, error) {
	return s.UploadBytes(ctx, projectID, filePath, []byte(content))
}

// UploadBytes сохраняет файл без индексации.
// Для PDF извлекается текстовый слой, остальные файлы должны быть корректным UTF-8 текстом.
func (s *Service) UploadBytes(ctx context.Context, projectID, filePath string, data []byte) (File, error) {
	if strings.TrimSpace(projectID) == "" {
		return File{}, fmt.Errorf("project_id не может быть пустым")
	}
	clean, err := normalizePath(filePath)
	if err != nil {
		return File{}, err
	}

	var content string
	if isPDF(clean, data) {
		content, err = extractPDFText(data)
		if err != nil {
			return File{}, fmt.Errorf("файл %q: %w", clean, err)
		}
	} else {
		if bytes.ContainsRune(data, 0) || !utf8.Valid(data) {
			return File{}, fmt.Errorf("файл %q не является корректным UTF-8 текстом (поддерживаются UTF-8 текст и PDF)", clean)
		}
		content = string(data)
	}

	f := File{
		ProjectID:     projectID,
		Path:          clean,
		Name:          path.Base(clean),
		ContentSHA256: hashText(content),
		SizeBytes:     len(data),
		Content:       content,
	}
	id, err := s.store.SaveFile(ctx, f)
	if err != nil {
		return File{}, err
	}
	f.ID = id
	return f, nil
}

// ListFiles возвращает файлы проекта.
func (s *Service) ListFiles(ctx context.Context, projectID string) ([]File, error) {
	return s.store.ListFiles(ctx, projectID)
}

// DeleteFile удаляет файл и его чанки.
func (s *Service) DeleteFile(ctx context.Context, projectID, filePath string) error {
	clean, err := normalizePath(filePath)
	if err != nil {
		return err
	}
	return s.store.DeleteFile(ctx, projectID, clean)
}

// Index индексирует файлы проекта. Пустая стратегия означает обе.
func (s *Service) Index(ctx context.Context, projectID, strategy string) ([]IndexRun, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, fmt.Errorf("project_id не может быть пустым")
	}
	if s.embedder == nil {
		return nil, fmt.Errorf("эмбеддер не настроен")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	files, err := s.store.ListFiles(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("в проекте %q нет загруженных файлов", projectID)
	}

	strategies := []string{chunk.StrategyFixed, chunk.StrategyStructure}
	if strategy != "" {
		normalized := normalizeStrategy(strategy)
		if normalized == "" {
			return nil, fmt.Errorf("неизвестная стратегия: %q", strategy)
		}
		strategies = []string{normalized}
	}

	var runs []IndexRun
	for _, st := range strategies {
		run, err := s.indexStrategy(ctx, projectID, st, files)
		if err != nil {
			return runs, fmt.Errorf("индексация %q: %w", st, err)
		}
		runs = append(runs, run)
	}

	if err := s.store.MarkProjectIndexed(ctx, projectID); err != nil {
		return runs, err
	}
	return runs, nil
}

func (s *Service) indexStrategy(ctx context.Context, projectID, strategy string, files []File) (IndexRun, error) {
	totalStart := time.Now()

	chunker, err := s.chunkerFor(strategy)
	if err != nil {
		return IndexRun{}, err
	}

	type item struct {
		fileID    int64
		sha       string
		chunkID   string
		section   string
		text      string
		index     int
		charCount int
		tokenEst  int
	}

	var (
		items     []item
		totalChar int
	)
	for _, f := range files {
		for _, c := range chunker.Chunk(chunk.File{Path: f.Path, Name: f.Name, Content: f.Content}) {
			items = append(items, item{
				fileID:    f.ID,
				sha:       hashText(c.Text),
				chunkID:   c.ChunkID,
				section:   c.Section,
				text:      c.Text,
				index:     c.Index,
				charCount: c.CharCount,
				tokenEst:  c.TokenEstimate,
			})
			totalChar += c.CharCount
		}
	}

	// Резолвим эмбеддинги: сначала кэш, затем батч-запрос к Ollama.
	embeddingIDs := make(map[string]int64)
	var (
		missTexts []string
		missSHAs  []string
	)
	for _, it := range items {
		if _, ok := embeddingIDs[it.sha]; ok {
			continue
		}
		id, _, found, err := s.store.GetEmbeddingByHash(ctx, it.sha, s.cfg.EmbedModel)
		if err != nil {
			return IndexRun{}, err
		}
		if found {
			embeddingIDs[it.sha] = id
			continue
		}
		embeddingIDs[it.sha] = 0
		missTexts = append(missTexts, it.text)
		missSHAs = append(missSHAs, it.sha)
	}

	embedStart := time.Now()
	if len(missTexts) > 0 {
		vectors, err := s.embedder.Embed(ctx, missTexts)
		if err != nil {
			return IndexRun{}, err
		}
		for i, sha := range missSHAs {
			id, err := s.store.InsertEmbedding(ctx, sha, s.cfg.EmbedModel, vectors[i])
			if err != nil {
				return IndexRun{}, err
			}
			embeddingIDs[sha] = id
		}
	}
	embedMS := time.Since(embedStart).Milliseconds()

	records := make([]ChunkRecord, 0, len(items))
	for _, it := range items {
		records = append(records, ChunkRecord{
			FileID:        it.fileID,
			Strategy:      strategy,
			ChunkID:       it.chunkID,
			Index:         it.index,
			Section:       it.section,
			Text:          it.text,
			CharCount:     it.charCount,
			TokenEstimate: it.tokenEst,
			EmbeddingID:   embeddingIDs[it.sha],
		})
	}

	if err := s.store.ReplaceChunks(ctx, projectID, strategy, records); err != nil {
		return IndexRun{}, err
	}

	avgChars := 0
	if len(records) > 0 {
		avgChars = totalChar / len(records)
	}
	run := IndexRun{
		ProjectID:     projectID,
		Strategy:      strategy,
		FileCount:     len(files),
		ChunkCount:    len(records),
		EmbedCount:    len(missTexts),
		CacheHits:     len(embeddingIDs) - len(missTexts),
		EmbedMS:       embedMS,
		TotalMS:       time.Since(totalStart).Milliseconds(),
		AvgChunkChars: avgChars,
	}
	if err := s.store.SaveIndexRun(ctx, run); err != nil {
		return IndexRun{}, err
	}
	return run, nil
}

// Search ищет top-K чанков по стратегии через общий retrieval-пайплайн.
// Совместимая обёртка над Retrieve.
func (s *Service) Search(ctx context.Context, projectID, strategy, queryText string, k int) ([]ScoredChunk, error) {
	return s.Retrieve(ctx, projectID, RetrieveOptions{
		Strategy: strategy,
		Query:    queryText,
		TopK:     k,
	})
}

// Retrieve выполняет полный retrieval: векторный поиск кандидатов,
// затем второй этап (порог/reranker) и обрезку до TopK.
func (s *Service) Retrieve(ctx context.Context, projectID string, opts RetrieveOptions) ([]ScoredChunk, error) {
	if strings.TrimSpace(opts.Query) == "" {
		return nil, fmt.Errorf("поисковый запрос не может быть пустым")
	}
	strategy := normalizeStrategy(opts.Strategy)
	if strategy == "" {
		return nil, fmt.Errorf("неизвестная стратегия: %q", opts.Strategy)
	}
	if s.embedder == nil {
		return nil, fmt.Errorf("эмбеддер не настроен")
	}

	vectors, err := s.embedder.Embed(ctx, []string{opts.Query})
	if err != nil {
		return nil, err
	}
	if len(vectors) != 1 {
		return nil, fmt.Errorf("не удалось получить эмбеддинг запроса")
	}

	chunks, err := s.store.ListChunks(ctx, projectID, strategy)
	if err != nil {
		return nil, err
	}

	return s.rank(chunks, vectors[0], opts.Query, s.resolveRankParams(opts.Candidates, opts.TopK, opts.Mode, opts.Threshold))
}

// rank применяет векторный поиск по кандидатам и второй этап (reranker).
// Векторы чанков и запроса нормализуются, чтобы Score был косинусной близостью.
func (s *Service) rank(chunks []StoredChunk, queryVec []float32, queryText string, p retrieveParams) ([]ScoredChunk, error) {
	query := normalizeVec(queryVec)
	for i := range chunks {
		chunks[i].Vector = normalizeVec(chunks[i].Vector)
	}

	candidates := SearchTopK(chunks, query, p.candidates)
	filtered, err := NewReranker(p.mode, p.threshold).Rerank(queryText, candidates)
	if err != nil {
		return nil, err
	}
	if p.topK > 0 && len(filtered) > p.topK {
		filtered = filtered[:p.topK]
	}
	return filtered, nil
}

// resolveRankParams подставляет значения по умолчанию из Config.
func (s *Service) resolveRankParams(candidates, topK int, mode string, threshold *float64) retrieveParams {
	if candidates <= 0 {
		candidates = s.cfg.Candidates
	}
	if candidates <= 0 {
		candidates = defaultCandidates
	}
	if topK <= 0 {
		topK = s.cfg.TopK
	}
	if topK <= 0 {
		topK = defaultTopK
	}
	if candidates < topK {
		candidates = topK
	}
	if strings.TrimSpace(mode) == "" {
		mode = s.cfg.RerankMode
	}
	thr := s.cfg.RerankThreshold
	if threshold != nil {
		thr = *threshold
	}
	return retrieveParams{candidates: candidates, topK: topK, mode: mode, threshold: thr}
}

// GetMetrics возвращает structural-метрики, последние запуски индексации и бенчмарки.
func (s *Service) GetMetrics(ctx context.Context, projectID string) (Metrics, error) {
	var m Metrics
	for _, st := range []string{chunk.StrategyFixed, chunk.StrategyStructure} {
		structural, err := s.store.Structural(ctx, projectID, st)
		if err != nil {
			return m, err
		}
		m.Structural = append(m.Structural, structural)
	}

	runs, err := s.store.LatestIndexRuns(ctx, projectID)
	if err != nil {
		return m, err
	}
	m.IndexRuns = runs

	bench, err := s.store.LatestBenchmarkRuns(ctx, projectID)
	if err != nil {
		return m, err
	}
	m.Benchmark = bench
	return m, nil
}

// ListQueries возвращает тест-запросы проекта.
func (s *Service) ListQueries(ctx context.Context, projectID string) ([]Query, error) {
	return s.store.ListQueries(ctx, projectID)
}

// AddQuery добавляет тест-запрос вручную.
func (s *Service) AddQuery(ctx context.Context, q Query) (Query, error) {
	if strings.TrimSpace(q.ProjectID) == "" {
		return Query{}, fmt.Errorf("project_id не может быть пустым")
	}
	if strings.TrimSpace(q.Text) == "" {
		return Query{}, fmt.Errorf("текст запроса не может быть пустым")
	}
	clean, err := normalizePath(q.ExpectedPath)
	if err != nil {
		return Query{}, fmt.Errorf("некорректный expected_path: %w", err)
	}
	q.ExpectedPath = clean
	if q.Source == "" {
		q.Source = "manual"
	}
	id, err := s.store.UpsertQuery(ctx, q)
	if err != nil {
		return Query{}, err
	}
	q.ID = id
	return q, nil
}

// DeleteQuery удаляет тест-запрос.
func (s *Service) DeleteQuery(ctx context.Context, projectID string, id int64) error {
	return s.store.DeleteQuery(ctx, projectID, id)
}

// GenerateQueries генерирует n тест-запросов из случайных чанков через LLM.
func (s *Service) GenerateQueries(ctx context.Context, projectID, strategy string, n int) (int, error) {
	if s.queryGen == nil {
		return 0, fmt.Errorf("генерация тест-запросов недоступна")
	}
	if n <= 0 {
		n = 5
	}
	strategy = normalizeStrategy(strategy)
	if strategy == "" {
		strategy = chunk.StrategyStructure
	}

	chunks, err := s.store.ListChunks(ctx, projectID, strategy)
	if err != nil {
		return 0, err
	}
	if len(chunks) == 0 {
		return 0, fmt.Errorf("нет проиндексированных чанков для стратегии %q", strategy)
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	order := rng.Perm(len(chunks))
	if n > len(chunks) {
		n = len(chunks)
	}

	generated := 0
	for _, idx := range order[:n] {
		question, err := s.queryGen(ctx, chunks[idx].Text)
		if err != nil {
			return generated, err
		}
		question = strings.TrimSpace(question)
		if question == "" {
			continue
		}
		_, err = s.store.UpsertQuery(ctx, Query{
			ProjectID:    projectID,
			Text:         question,
			ExpectedPath: chunks[idx].Path,
			Source:       "auto_llm",
		})
		if err != nil {
			return generated, err
		}
		generated++
	}
	return generated, nil
}

// Benchmark оценивает обе стратегии на тест-запросах и сохраняет результаты.
// Использует тот же retrieval-пайплайн, что и чат/поиск (кандидаты → порог → topK).
func (s *Service) Benchmark(ctx context.Context, projectID string, opts RetrieveOptions) ([]BenchmarkRun, error) {
	if s.embedder == nil {
		return nil, fmt.Errorf("эмбеддер не настроен")
	}

	queries, err := s.store.ListQueries(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if len(queries) == 0 {
		return nil, fmt.Errorf("нет тест-запросов, добавьте вручную или сгенерируйте")
	}

	texts := make([]string, len(queries))
	for i, q := range queries {
		texts[i] = q.Text
	}
	queryVectors, err := s.embedder.Embed(ctx, texts)
	if err != nil {
		return nil, err
	}
	for i := range queryVectors {
		queryVectors[i] = normalizeVec(queryVectors[i])
	}

	p := s.resolveRankParams(opts.Candidates, opts.TopK, opts.Mode, opts.Threshold)
	k := p.topK

	var runs []BenchmarkRun
	for _, strategy := range []string{chunk.StrategyFixed, chunk.StrategyStructure} {
		chunks, err := s.store.ListChunks(ctx, projectID, strategy)
		if err != nil {
			return nil, err
		}
		if len(chunks) == 0 {
			continue
		}

		results := make([]QueryResult, 0, len(queries))
		for i, q := range queries {
			top, err := s.rank(chunks, queryVectors[i], q.Text, p)
			if err != nil {
				return nil, err
			}
			first := 0
			matches := 0
			for rank, sc := range top {
				if queryMatches(q, sc) {
					matches++
					if first == 0 {
						first = rank + 1
					}
				}
			}
			results = append(results, QueryResult{Hit: first > 0, FirstRank: first, Matches: matches})
		}

		recall, precision, mrr := Evaluate(results, k)
		run := BenchmarkRun{
			ProjectID:  projectID,
			Strategy:   strategy,
			K:          k,
			QueryCount: len(queries),
			Recall:     recall,
			Precision:  precision,
			MRR:        mrr,
		}
		if err := s.store.SaveBenchmarkRun(ctx, run); err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}

	if len(runs) == 0 {
		return nil, fmt.Errorf("нет проиндексированных чанков, сначала выполните индексацию")
	}
	return runs, nil
}

func (s *Service) chunkerFor(strategy string) (chunk.Chunker, error) {
	switch normalizeStrategy(strategy) {
	case chunk.StrategyFixed:
		return s.fixed, nil
	case chunk.StrategyStructure:
		return s.structure, nil
	default:
		return nil, fmt.Errorf("неизвестная стратегия: %q", strategy)
	}
}

func queryMatches(q Query, sc ScoredChunk) bool {
	if sc.Path != q.ExpectedPath {
		return false
	}
	if q.ExpectedSection == "" {
		return true
	}
	return sc.Section == q.ExpectedSection
}

func normalizeStrategy(strategy string) string {
	switch strings.ToLower(strings.TrimSpace(strategy)) {
	case chunk.StrategyFixed:
		return chunk.StrategyFixed
	case chunk.StrategyStructure:
		return chunk.StrategyStructure
	default:
		return ""
	}
}

func normalizePath(p string) (string, error) {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	p = strings.TrimLeft(p, "/")
	if p == "" {
		return "", fmt.Errorf("путь файла не может быть пустым")
	}
	if strings.Contains(p, "..") {
		return "", fmt.Errorf("путь не может содержать '..'")
	}
	return p, nil
}

func hashText(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}
