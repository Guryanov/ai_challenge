// Package digest реализует фоновую генерацию ежедневной сводки.
package digest

import (
	"fmt"
	"log"
	"sync"
	"time"

	"ai-chat/internal/agent"
)

// AgentRunner — минимальный интерфейс агента, необходимый для генерации сводки.
type AgentRunner interface {
	Run(req agent.AgentRequest) (agent.AgentResponse, error)
}

// Scheduler запускает генерацию сводки по расписанию.
type Scheduler struct {
	runner  AgentRunner
	store   Store
	prompt  string
	runAt   string
	enabled bool

	mu       sync.Mutex
	stopChan chan struct{}
	wg       sync.WaitGroup
}

// NewScheduler создаёт новый планировщик.
// runAt задаётся в формате "HH:MM" в локальном времени сервера.
func NewScheduler(runner AgentRunner, store Store, prompt, runAt string, enabled bool) *Scheduler {
	return &Scheduler{
		runner:  runner,
		store:   store,
		prompt:  prompt,
		runAt:   runAt,
		enabled: enabled,
	}
}

// Start запускает планировщик в фоновой горутине.
func (s *Scheduler) Start() {
	if !s.enabled {
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

// Stop останавливает планировщик и дожидается завершения текущей итерации.
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

	// Ожидаем ближайшее запланированное время, затем запускаемся.
	firstWait, err := timeUntil(s.runAt, time.Now())
	if err != nil {
		log.Printf("[digest] ошибка расписания %q: %v", s.runAt, err)
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

func (s *Scheduler) stopSignal() <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopChan == nil {
		return make(chan struct{})
	}
	return s.stopChan
}

func (s *Scheduler) run() {
	log.Printf("[digest] запуск генерации ежедневной сводки")
	start := time.Now()
	if err := generate(s.runner, s.store, s.prompt); err != nil {
		log.Printf("[digest] ошибка генерации сводки: %v", err)
		return
	}
	log.Printf("[digest] сводка сгенерирована за %s", time.Since(start))
}

// generate вызывает LLM через агента и сохраняет результат.
func generate(runner AgentRunner, store Store, prompt string) error {
	req := agent.AgentRequest{
		Message:        prompt,
		WorkflowMode:   agent.WorkflowModeChat,
		ResponseFormat: "text",
	}

	resp, err := runner.Run(req)
	if err != nil {
		if saveErr := store.Save(Digest{Error: err.Error(), GeneratedAt: time.Now().UTC()}); saveErr != nil {
			return fmt.Errorf("ошибка генерации сводки: %w; не удалось сохранить ошибку: %v", err, saveErr)
		}
		return fmt.Errorf("ошибка генерации сводки: %w", err)
	}

	d := Digest{
		Content:     resp.Content,
		GeneratedAt: time.Now().UTC(),
	}
	if err := store.Save(d); err != nil {
		return fmt.Errorf("не удалось сохранить сводку: %w", err)
	}

	return nil
}

// timeUntil возвращает длительность до ближайшего момента времени hh:mm относительно now.
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
