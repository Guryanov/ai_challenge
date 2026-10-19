package pipeline

import (
	"testing"
	"time"
)

func TestTimeUntil(t *testing.T) {
	now := time.Date(2026, 10, 19, 8, 0, 0, 0, time.Local)

	d, err := timeUntil("09:00", now)
	if err != nil {
		t.Fatalf("timeUntil: %v", err)
	}
	if d != time.Hour {
		t.Fatalf("duration = %s, want 1h", d)
	}

	// Время уже прошло — ждём до следующего дня.
	d, err = timeUntil("07:00", now)
	if err != nil {
		t.Fatalf("timeUntil: %v", err)
	}
	if d != 23*time.Hour {
		t.Fatalf("duration = %s, want 23h", d)
	}

	if _, err := timeUntil("bad", now); err == nil {
		t.Fatal("expected error for invalid time")
	}
}
