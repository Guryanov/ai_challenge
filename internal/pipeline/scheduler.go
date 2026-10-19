package pipeline

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// Runner — источник запусков конвейера (реализуется Orchestrator).
type Runner interface {
	RunOnce(ctx context.Context, dryRun bool) (RunResult, error)
}

// Scheduler запускает конвейер раз в сутки в заданное время HH:MM.
type Scheduler struct {
	runner  Runner
	at      string
	enabled bool

	mu       sync.Mutex
	stopChan chan struct{}
	wg       sync.WaitGroup
}

// NewScheduler создаёт планировщик.
// at задаётся в формате "HH:MM" в локальном времени сервера.
func NewScheduler(runner Runner, at string, enabled bool) *Scheduler {
	return &Scheduler{runner: runner, at: at, enabled: enabled}
}

// Start запускает планировщик в фоновой горутине.
func (s *Scheduler) Start() {
	if !s.enabled || s.runner == nil {
		return
	}

	s.mu.Lock()
	if s.stopChan != nil {
		s.mu.Unlock()
		return
	}
	s.stopChan = make(chan struct{})
	s.mu.Unlock()

	s.wg.Add(1)
	go s.loop()
}

// Stop останавливает планировщик и ждёт завершения текущей итерации.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if s.stopChan == nil {
		s.mu.Unlock()
		return
	}
	close(s.stopChan)
	s.stopChan = nil
	s.mu.Unlock()

	s.wg.Wait()
}

func (s *Scheduler) loop() {
	defer s.wg.Done()

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	firstWait, err := timeUntil(s.at, time.Now())
	if err != nil {
		log.Printf("[pipeline] ошибка расписания %q: %v", s.at, err)
		return
	}

	select {
	case <-time.After(firstWait):
		s.run()
	case <-s.stopSignal():
		return
	}

	for {
		select {
		case <-ticker.C:
			s.run()
		case <-s.stopSignal():
			return
		}
	}
}

func (s *Scheduler) run() {
	log.Printf("[pipeline] запуск по расписанию")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	res, err := s.runner.RunOnce(ctx, false)
	if err != nil {
		log.Printf("[pipeline] ошибка запуска по расписанию: %v", err)
		return
	}
	log.Printf("[pipeline] запуск завершён: найдено %d, обработано %d", res.Fetched, res.Processed)
}

func (s *Scheduler) stopSignal() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopChan == nil {
		return make(chan struct{})
	}
	return s.stopChan
}

// timeUntil возвращает длительность до ближайшего времени hh:mm относительно now.
func timeUntil(hhmm string, now time.Time) (time.Duration, error) {
	t, err := time.Parse("15:04", hhmm)
	if err != nil {
		return 0, fmt.Errorf("неверный формат времени %q: %w", hhmm, err)
	}

	local := now.Local()
	next := time.Date(local.Year(), local.Month(), local.Day(), t.Hour(), t.Minute(), 0, 0, local.Location())
	if !next.After(local) {
		next = next.Add(24 * time.Hour)
	}

	return next.Sub(local), nil
}
