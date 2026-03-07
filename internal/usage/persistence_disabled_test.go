package usage

import "testing"

func TestPersistenceManagerDisabledWithoutPath(t *testing.T) {
	manager := NewPersistenceManager(NewRequestStatistics(), "")

	if manager.Enabled() {
		t.Fatalf("Enabled() = true, want false")
	}
	if err := manager.Flush(); err != nil {
		t.Fatalf("Flush() error = %v, want nil", err)
	}
	if _, err := manager.Load(); err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
}
