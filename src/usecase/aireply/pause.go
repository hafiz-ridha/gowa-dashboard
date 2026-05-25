package aireply

import (
	"sync/atomic"
	"time"
)

// Global pause untuk seluruh AI Reply (semua device + semua chat).
//
// State disimpan in-memory via atomic pointer ke time.Time. Restart container
// = otomatis resume — itu sengaja, supaya tidak ada "paused selamanya yang
// terlupa" stuck di DB. Kalau perlu pause permanent, user set
// AI_REPLY_ENABLED=false di src/.env (yang sudah ada).
//
// Suppression behavior: saat paused, HandleIncoming return true (claim
// ownership), jadi static WHATSAPP_AUTO_REPLY juga tidak fire — total silent.
var globalPauseUntil atomic.Pointer[time.Time]

// IsPaused — cek apakah AI Reply sedang di-pause global.
// Auto-clear pointer kalau deadline sudah lewat (lazy cleanup).
func IsPaused() bool {
	t := globalPauseUntil.Load()
	if t == nil {
		return false
	}
	if time.Now().After(*t) {
		// Deadline lewat, clear pointer supaya panggilan berikutnya cepat.
		globalPauseUntil.CompareAndSwap(t, nil)
		return false
	}
	return true
}

// Pause AI Reply selama duration. duration <= 0 = pause tak terbatas
// (clamped jadi 100 tahun supaya tetap representable). Return deadline.
func Pause(duration time.Duration) time.Time {
	var until time.Time
	if duration <= 0 {
		until = time.Now().AddDate(100, 0, 0)
	} else {
		until = time.Now().Add(duration)
	}
	globalPauseUntil.Store(&until)
	return until
}

// Resume — clear pause state. Aman dipanggil saat tidak sedang paused.
func Resume() {
	globalPauseUntil.Store(nil)
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
