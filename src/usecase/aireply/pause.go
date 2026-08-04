package aireply

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"

	infraAI "github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/aireply"
)

// Global pause untuk seluruh AI Reply (semua device + semua chat).
//
// State di-cache in-memory via atomic pointer supaya hot path (satu
// IsPaused() check per pesan WhatsApp masuk) tidak perlu round-trip DB.
// Sumber kebenaran tetap tabel ai_pause_state: di-hydrate sekali saat startup
// (LoadPersistedPause) dan ditulis lagi setiap Pause()/Resume() (best-effort —
// gagal tulis DB tidak menggagalkan request, in-memory state tetap berlaku
// untuk proses yang sedang jalan).
//
// Sengaja PERSISTEN lintas restart: admin yang men-pause auto-reply
// mengharapkan itu tetap ter-pause sampai dia sendiri yang resume, termasuk
// setelah `docker compose up -d --build` untuk update core/dashboard.
var globalPauseUntil atomic.Pointer[time.Time]

// pauseRepo persists the pause deadline. nil-safe: pause still works
// in-memory-only (pre-persistence behaviour) if never set, e.g. in tests.
var pauseRepo *infraAI.Repository

// SetPauseStore wires the persistence backend. Call once at startup, before
// LoadPersistedPause.
func SetPauseStore(r *infraAI.Repository) {
	pauseRepo = r
}

// LoadPersistedPause hydrates the in-memory cache from storage. Call once at
// startup, after SetPauseStore and before the server starts handling
// incoming messages, so no message can slip through unpaused during boot.
func LoadPersistedPause(ctx context.Context) error {
	if pauseRepo == nil {
		return nil
	}
	until, err := pauseRepo.GetPauseUntil(ctx)
	if err != nil {
		return err
	}
	if until != nil && time.Now().Before(*until) {
		globalPauseUntil.Store(until)
	}
	return nil
}

// IsPaused — cek apakah AI Reply sedang di-pause global.
// Auto-clear pointer kalau deadline sudah lewat (lazy cleanup).
func IsPaused() bool {
	t := globalPauseUntil.Load()
	if t == nil {
		return false
	}
	if time.Now().After(*t) {
		// Deadline lewat, clear pointer supaya panggilan berikutnya cepat.
		// DB row dibiarkan basi — LoadPersistedPause di restart berikutnya
		// re-check timestamp-nya sebelum hydrate, jadi aman.
		globalPauseUntil.CompareAndSwap(t, nil)
		return false
	}
	return true
}

// Pause AI Reply selama duration. duration <= 0 = pause tak terbatas
// (clamped jadi 100 tahun supaya tetap representable). Return deadline.
// Persists to storage (best-effort) so the pause survives a restart.
func Pause(ctx context.Context, duration time.Duration) time.Time {
	var until time.Time
	if duration <= 0 {
		until = time.Now().AddDate(100, 0, 0)
	} else {
		until = time.Now().Add(duration)
	}
	globalPauseUntil.Store(&until)
	persist(ctx, &until)
	return until
}

// Resume — clear pause state. Aman dipanggil saat tidak sedang paused.
func Resume(ctx context.Context) {
	globalPauseUntil.Store(nil)
	persist(ctx, nil)
}

// PauseStatus — return (paused, deadline). deadline nil kalau tidak paused.
func PauseStatus() (bool, *time.Time) {
	t := globalPauseUntil.Load()
	if t == nil {
		return false, nil
	}
	if time.Now().After(*t) {
		globalPauseUntil.CompareAndSwap(t, nil)
		return false, nil
	}
	return true, t
}

func persist(ctx context.Context, until *time.Time) {
	if pauseRepo == nil {
		return
	}
	if err := pauseRepo.SetPauseUntil(ctx, until); err != nil {
		logrus.Warnf("AI Reply: failed to persist pause state (%v); in-memory state still applied for this process", err)
	}
}
