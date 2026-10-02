package readiness

import (
	"github.com/example/google-project-cost-manager/internal/config"
	"testing"
	"time"
)

func TestReadinessRequiresDistinctDaysAndCurrentConfiguration(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	cfg := &config.Config{ControlProjectID: "control", ReleaseID: "digest", InventoryObservedAt: now}
	e := Evidence{Days: map[string]bool{}, ToggleProject: "disposable", ToggleAt: now}
	for i := 0; i < 7; i++ {
		e.Days[now.AddDate(0, 0, -i).Format("2006-01-02")] = true
	}
	if err := Check(e, cfg, now); err != nil {
		t.Fatal(err)
	}
	e.Days["2026-10-01"] = false
	if Check(e, cfg, now) == nil {
		t.Fatal("failed day accepted")
	}
	e.Days["2026-10-01"] = true
	e.Days["2026-10-02"] = false
	if Check(e, cfg, now) == nil {
		t.Fatal("today's failure must invalidate yesterday's streak")
	}
	cfg.InventoryObservedAt = now.Add(-49 * time.Hour)
	if Check(e, cfg, now) == nil {
		t.Fatal("stale inventory accepted")
	}
	cfg.InventoryObservedAt = now
	key := Fingerprint(cfg)
	cfg.ReleaseID = "new-release"
	if key == Fingerprint(cfg) {
		t.Fatal("release change did not invalidate evidence")
	}
}
