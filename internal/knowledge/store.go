// Package knowledge реализует базу знаний: SQLite-хранилище чанков,
// эмбеддингов и метрик, а также сервис индексации и поиска.
package knowledge

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// File — загруженный текстовый файл.
type File struct {
	ID            int64  `json:"id"`
	ProjectID     string `json:"project_id"`
	Path          string `json:"path"`
	Name          string `json:"name"`
	ContentSHA256 string `json:"content_sha256"`
	SizeBytes     int    `json:"size_bytes"`
	Content       string `json:"-"`
	IndexedAt     string `json:"indexed_at"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

// StoredChunk — чанк, сохранённый в БД.
type StoredChunk struct {
	ID            int64
	FileID        int64
	ProjectID     string
	Strategy      string
	ChunkID       string
	Index         int
	Section       string
	Text          string
	CharCount     int
	TokenEstimate int
	EmbeddingID   int64
	Path          string
	Name          string
	Vector        []float32
}

// ChunkRecord — чанк для вставки (без вектора).
type ChunkRecord struct {
	FileID        int64
	Strategy      string
	ChunkID       string
	Index         int
	Section       string
	Text          string
	CharCount     int
	TokenEstimate int
	EmbeddingID   int64
}

// Query — тест-запрос для оценки retrieval.
type Query struct {
	ID              int64  `json:"id"`
	ProjectID       string `json:"project_id"`
	Text            string `json:"text"`
	ExpectedPath    string `json:"expected_path"`
	ExpectedSection string `json:"expected_section"`
	Source          string `json:"source"`
	CreatedAt       string `json:"created_at"`
}

// IndexRun — метрики одной операции индексации.
type IndexRun struct {
	ID            int64  `json:"id"`
	ProjectID     string `json:"project_id"`
	Strategy      string `json:"strategy"`
	FileCount     int    `json:"file_count"`
	ChunkCount    int    `json:"chunk_count"`
	EmbedCount    int    `json:"embed_count"`
	CacheHits     int    `json:"cache_hits"`
	EmbedMS       int64  `json:"embed_ms"`
	TotalMS       int64  `json:"total_ms"`
	AvgChunkChars int    `json:"avg_chunk_chars"`
	CreatedAt     string `json:"created_at"`
}

// BenchmarkRun — результаты оценки стратегии на тест-запросах.
type BenchmarkRun struct {
	ID         int64   `json:"id"`
	ProjectID  string  `json:"project_id"`
	Strategy   string  `json:"strategy"`
	K          int     `json:"k"`
	QueryCount int     `json:"query_count"`
	Recall     float64 `json:"recall"`
	Precision  float64 `json:"precision"`
	MRR        float64 `json:"mrr"`
	CreatedAt  string  `json:"created_at"`
}

// Structural — количественные характеристики индекса стратегии.
type Structural struct {
	Strategy     string  `json:"strategy"`
	FileCount    int     `json:"file_count"`
	ChunkCount   int     `json:"chunk_count"`
	SectionCount int     `json:"section_count"`
	MinChars     int     `json:"min_chars"`
	AvgChars     float64 `json:"avg_chars"`
	MaxChars     int     `json:"max_chars"`
	StdDevChars  float64 `json:"std_dev_chars"`
	AvgTokens    float64 `json:"avg_tokens"`
	TotalChars   int     `json:"total_chars"`
}

// Store — SQLite-хранилище базы знаний.
type Store struct {
	db *sql.DB
}

// NewStore открывает (или создаёт) БД по указанному пути.
func NewStore(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("не удалось создать каталог индекса %q: %w", dir, err)
		}
	}

	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть БД индекса: %w", err)
	}
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close закрывает БД.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS files (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id TEXT NOT NULL,
			path TEXT NOT NULL,
			name TEXT NOT NULL,
			content_sha256 TEXT NOT NULL,
			size_bytes INTEGER NOT NULL,
			content TEXT NOT NULL,
			indexed_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			UNIQUE(project_id, path)
		)`,
		`CREATE TABLE IF NOT EXISTS embeddings (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			sha256 TEXT NOT NULL,
			model TEXT NOT NULL,
			dim INTEGER NOT NULL,
			vector BLOB NOT NULL,
			created_at TEXT NOT NULL,
			UNIQUE(sha256, model)
		)`,
		`CREATE TABLE IF NOT EXISTS chunks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			file_id INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
			project_id TEXT NOT NULL,
			strategy TEXT NOT NULL,
			chunk_id TEXT NOT NULL,
			idx INTEGER NOT NULL,
			section TEXT NOT NULL,
			text TEXT NOT NULL,
			char_count INTEGER NOT NULL,
			token_estimate INTEGER NOT NULL,
			embedding_id INTEGER NOT NULL REFERENCES embeddings(id),
			UNIQUE(project_id, strategy, chunk_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_chunks_project_strategy ON chunks(project_id, strategy)`,
		`CREATE TABLE IF NOT EXISTS index_runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id TEXT NOT NULL,
			strategy TEXT NOT NULL,
			file_count INTEGER NOT NULL,
			chunk_count INTEGER NOT NULL,
			embed_count INTEGER NOT NULL,
			cache_hits INTEGER NOT NULL,
			embed_ms INTEGER NOT NULL,
			total_ms INTEGER NOT NULL,
			avg_chunk_chars INTEGER NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS queries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id TEXT NOT NULL,
			text TEXT NOT NULL,
			expected_path TEXT NOT NULL,
			expected_section TEXT NOT NULL DEFAULT '',
			source TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS benchmark_runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			project_id TEXT NOT NULL,
			strategy TEXT NOT NULL,
			k INTEGER NOT NULL,
			query_count INTEGER NOT NULL,
			recall REAL NOT NULL,
			precision REAL NOT NULL,
			mrr REAL NOT NULL,
			created_at TEXT NOT NULL
		)`,
	}

	for _, stmt := range statements {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("ошибка миграции БД индекса: %w", err)
		}
	}
	return nil
}

// SaveFile сохраняет (или заменяет) файл и удаляет его устаревшие чанки.
func (s *Store) SaveFile(ctx context.Context, f File) (int64, error) {
	now := nowString()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var id int64
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM files WHERE project_id = ? AND path = ?`, f.ProjectID, f.Path).Scan(&id)
	switch {
	case err == sql.ErrNoRows:
		res, err := tx.ExecContext(ctx,
			`INSERT INTO files (project_id, path, name, content_sha256, size_bytes, content, indexed_at, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, NULL, ?, ?)`,
			f.ProjectID, f.Path, f.Name, f.ContentSHA256, f.SizeBytes, f.Content, now, now)
		if err != nil {
			return 0, fmt.Errorf("не удалось сохранить файл: %w", err)
		}
		id, _ = res.LastInsertId()
	case err != nil:
		return 0, err
	default:
		if _, err := tx.ExecContext(ctx,
			`UPDATE files SET name = ?, content_sha256 = ?, size_bytes = ?, content = ?, indexed_at = NULL, updated_at = ?
			 WHERE id = ?`,
			f.Name, f.ContentSHA256, f.SizeBytes, f.Content, now, id); err != nil {
			return 0, fmt.Errorf("не удалось обновить файл: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM chunks WHERE file_id = ?`, id); err != nil {
			return 0, fmt.Errorf("не удалось удалить устаревшие чанки: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// ListFiles возвращает файлы проекта.
func (s *Store) ListFiles(ctx context.Context, projectID string) ([]File, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, path, name, content_sha256, size_bytes, content, COALESCE(indexed_at, ''), created_at, updated_at
		 FROM files WHERE project_id = ? ORDER BY path`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []File
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.ID, &f.ProjectID, &f.Path, &f.Name, &f.ContentSHA256, &f.SizeBytes,
			&f.Content, &f.IndexedAt, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// MarkProjectIndexed помечает все файлы проекта как проиндексированные.
func (s *Store) MarkProjectIndexed(ctx context.Context, projectID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE files SET indexed_at = ? WHERE project_id = ?`, nowString(), projectID)
	return err
}

// DeleteFile удаляет файл и его чанки.
func (s *Store) DeleteFile(ctx context.Context, projectID, path string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM files WHERE project_id = ? AND path = ?`, projectID, path)
	if err != nil {
		return fmt.Errorf("не удалось удалить файл: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("файл %q не найден", path)
	}
	return nil
}

// GetEmbeddingByHash ищет кэшированный эмбеддинг по хешу текста и модели.
func (s *Store) GetEmbeddingByHash(ctx context.Context, sha, model string) (int64, []float32, bool, error) {
	var (
		id  int64
		blb []byte
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT id, vector FROM embeddings WHERE sha256 = ? AND model = ?`, sha, model).Scan(&id, &blb)
	if err == sql.ErrNoRows {
		return 0, nil, false, nil
	}
	if err != nil {
		return 0, nil, false, err
	}
	return id, decodeVector(blb), true, nil
}

// InsertEmbedding сохраняет эмбеддинг и возвращает его id.
func (s *Store) InsertEmbedding(ctx context.Context, sha, model string, vec []float32) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO embeddings (sha256, model, dim, vector, created_at) VALUES (?, ?, ?, ?, ?)`,
		sha, model, len(vec), encodeVector(vec), nowString())
	if err != nil {
		return 0, fmt.Errorf("не удалось сохранить эмбеддинг: %w", err)
	}
	return res.LastInsertId()
}

// ReplaceChunks атомарно заменяет все чанки проекта для стратегии.
func (s *Store) ReplaceChunks(ctx context.Context, projectID, strategy string, chunks []ChunkRecord) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM chunks WHERE project_id = ? AND strategy = ?`, projectID, strategy); err != nil {
		return fmt.Errorf("не удалось очистить старые чанки: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO chunks (file_id, project_id, strategy, chunk_id, idx, section, text, char_count, token_estimate, embedding_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, c := range chunks {
		if _, err := stmt.ExecContext(ctx, c.FileID, projectID, strategy, c.ChunkID, c.Index, c.Section,
			c.Text, c.CharCount, c.TokenEstimate, c.EmbeddingID); err != nil {
			return fmt.Errorf("не удалось сохранить чанк: %w", err)
		}
	}

	return tx.Commit()
}

// ListChunks возвращает чанки проекта для стратегии вместе с векторами.
func (s *Store) ListChunks(ctx context.Context, projectID, strategy string) ([]StoredChunk, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT c.id, c.file_id, c.project_id, c.strategy, c.chunk_id, c.idx, c.section, c.text,
		        c.char_count, c.token_estimate, c.embedding_id, f.path, f.name, e.vector
		 FROM chunks c
		 JOIN files f ON f.id = c.file_id
		 JOIN embeddings e ON e.id = c.embedding_id
		 WHERE c.project_id = ? AND c.strategy = ?
		 ORDER BY f.path, c.idx`, projectID, strategy)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chunks []StoredChunk
	for rows.Next() {
		var (
			c   StoredChunk
			blb []byte
		)
		if err := rows.Scan(&c.ID, &c.FileID, &c.ProjectID, &c.Strategy, &c.ChunkID, &c.Index, &c.Section,
			&c.Text, &c.CharCount, &c.TokenEstimate, &c.EmbeddingID, &c.Path, &c.Name, &blb); err != nil {
			return nil, err
		}
		c.Vector = decodeVector(blb)
		chunks = append(chunks, c)
	}
	return chunks, rows.Err()
}

// Structural считает количественные метрики стратегии.
func (s *Store) Structural(ctx context.Context, projectID, strategy string) (Structural, error) {
	st := Structural{Strategy: strategy}

	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT file_id), COUNT(*), COUNT(DISTINCT section),
		        COALESCE(MIN(char_count), 0), COALESCE(AVG(char_count), 0), COALESCE(MAX(char_count), 0),
		        COALESCE(SUM(char_count), 0), COALESCE(AVG(token_estimate), 0)
		 FROM chunks WHERE project_id = ? AND strategy = ?`,
		projectID, strategy).Scan(&st.FileCount, &st.ChunkCount, &st.SectionCount,
		&st.MinChars, &st.AvgChars, &st.MaxChars, &st.TotalChars, &st.AvgTokens); err != nil {
		return st, err
	}

	if st.ChunkCount > 0 {
		var variance float64
		if err := s.db.QueryRowContext(ctx,
			`SELECT COALESCE(AVG((char_count - ?) * (char_count - ?)), 0) FROM chunks WHERE project_id = ? AND strategy = ?`,
			st.AvgChars, st.AvgChars, projectID, strategy).Scan(&variance); err != nil {
			return st, err
		}
		st.StdDevChars = math.Sqrt(variance)
	}

	if st.ChunkCount == 0 {
		st.FileCount = 0
	}
	return st, nil
}

// SaveIndexRun сохраняет метрики индексации.
func (s *Store) SaveIndexRun(ctx context.Context, run IndexRun) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO index_runs (project_id, strategy, file_count, chunk_count, embed_count, cache_hits,
		                         embed_ms, total_ms, avg_chunk_chars, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ProjectID, run.Strategy, run.FileCount, run.ChunkCount, run.EmbedCount, run.CacheHits,
		run.EmbedMS, run.TotalMS, run.AvgChunkChars, nowString())
	return err
}

// LatestIndexRuns возвращает последний запуск индексации по каждой стратегии.
func (s *Store) LatestIndexRuns(ctx context.Context, projectID string) ([]IndexRun, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, strategy, file_count, chunk_count, embed_count, cache_hits,
		        embed_ms, total_ms, avg_chunk_chars, created_at
		 FROM index_runs r
		 WHERE project_id = ? AND id = (SELECT MAX(id) FROM index_runs WHERE project_id = r.project_id AND strategy = r.strategy)
		 ORDER BY strategy`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []IndexRun
	for rows.Next() {
		var r IndexRun
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Strategy, &r.FileCount, &r.ChunkCount, &r.EmbedCount,
			&r.CacheHits, &r.EmbedMS, &r.TotalMS, &r.AvgChunkChars, &r.CreatedAt); err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// ListQueries возвращает тест-запросы проекта.
func (s *Store) ListQueries(ctx context.Context, projectID string) ([]Query, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, text, expected_path, expected_section, source, created_at
		 FROM queries WHERE project_id = ? ORDER BY id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var queries []Query
	for rows.Next() {
		var q Query
		if err := rows.Scan(&q.ID, &q.ProjectID, &q.Text, &q.ExpectedPath, &q.ExpectedSection, &q.Source, &q.CreatedAt); err != nil {
			return nil, err
		}
		queries = append(queries, q)
	}
	return queries, rows.Err()
}

// UpsertQuery добавляет тест-запрос.
func (s *Store) UpsertQuery(ctx context.Context, q Query) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO queries (project_id, text, expected_path, expected_section, source, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		q.ProjectID, q.Text, q.ExpectedPath, q.ExpectedSection, q.Source, nowString())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// DeleteQuery удаляет тест-запрос.
func (s *Store) DeleteQuery(ctx context.Context, projectID string, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM queries WHERE project_id = ? AND id = ?`, projectID, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("запрос %d не найден", id)
	}
	return nil
}

// SaveBenchmarkRun сохраняет результат бенчмарка.
func (s *Store) SaveBenchmarkRun(ctx context.Context, r BenchmarkRun) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO benchmark_runs (project_id, strategy, k, query_count, recall, precision, mrr, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ProjectID, r.Strategy, r.K, r.QueryCount, r.Recall, r.Precision, r.MRR, nowString())
	return err
}

// LatestBenchmarkRuns возвращает последний бенчмарк по каждой стратегии.
func (s *Store) LatestBenchmarkRuns(ctx context.Context, projectID string) ([]BenchmarkRun, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, strategy, k, query_count, recall, precision, mrr, created_at
		 FROM benchmark_runs r
		 WHERE project_id = ? AND id = (SELECT MAX(id) FROM benchmark_runs WHERE project_id = r.project_id AND strategy = r.strategy)
		 ORDER BY strategy`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var runs []BenchmarkRun
	for rows.Next() {
		var r BenchmarkRun
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Strategy, &r.K, &r.QueryCount, &r.Recall, &r.Precision, &r.MRR, &r.CreatedAt); err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

func nowString() string { return time.Now().UTC().Format(time.RFC3339Nano) }

func encodeVector(vec []float32) []byte {
	buf := make([]byte, 4*len(vec))
	for i, v := range vec {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	return buf
}

func decodeVector(b []byte) []float32 {
	if len(b)%4 != 0 {
		return nil
	}
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out
}
