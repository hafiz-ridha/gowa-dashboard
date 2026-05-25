// Package broadcast melakukan pengiriman pesan ke banyak nomor secara
// sequential dengan strategi anti-spam:
//
//   - Random delay antar pesan (user-configurable min/max)
//   - Batch break (istirahat lebih lama tiap N pesan)
//   - Random shuffle urutan recipient (opsional)
//   - Spintax / message variants (opsional, via {a|b|c} di body)
//   - Skip duplikat di parse-level
//
// Worker per-broadcast (goroutine), bisa di-cancel kapan saja.
package broadcast

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/dashboard/internal/store"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/dashboard/internal/wa"
)

type Broadcaster struct {
	store *store.Store
	wa    *wa.Client

	mu      sync.Mutex
	cancels map[int64]context.CancelFunc
}

func New(s *store.Store, w *wa.Client) *Broadcaster {
	return &Broadcaster{
		store:   s,
		wa:      w,
		cancels: map[int64]context.CancelFunc{},
	}
}

// Resume saat dashboard start — tandai broadcast running yg tertinggal
// dari proses sebelumnya jadi cancelled (mereka tidak bisa di-resume
// karena state in-memory ilang).
func (b *Broadcaster) Resume() error {
	return b.store.MarkOrphanedRunningBroadcastsAsCancelled()
}

// Start broadcast — return langsung, worker jalan di goroutine.
// Caller harus pastikan broadcast belum running (cek di handler).
func (b *Broadcaster) Start(id int64) error {
	bc, err := b.store.GetBroadcast(id)
	if err != nil {
		return err
	}
	if bc.Status == "running" {
		return fmt.Errorf("broadcast already running")
	}
	if bc.Total <= 0 {
		return fmt.Errorf("broadcast has no recipients")
	}

	b.mu.Lock()
	if _, exists := b.cancels[id]; exists {
		b.mu.Unlock()
		return fmt.Errorf("broadcast already running")
	}
	ctx, cancel := context.WithCancel(context.Background())
	b.cancels[id] = cancel
	b.mu.Unlock()

	go b.run(ctx, id)
	return nil
}

// Cancel running broadcast. Tidak error kalau broadcast tidak running
// (idempotent — UI bisa panggil tanpa khawatir race).
func (b *Broadcaster) Cancel(id int64) error {
	b.mu.Lock()
	cancel, ok := b.cancels[id]
	if ok {
		delete(b.cancels, id)
	}
	b.mu.Unlock()
	if !ok {
		return nil
	}
	cancel()
	return nil
}

// IsRunning untuk cek di handler sebelum delete.
func (b *Broadcaster) IsRunning(id int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, ok := b.cancels[id]
	return ok
}

func (b *Broadcaster) run(ctx context.Context, id int64) {
	defer func() {
		b.mu.Lock()
		delete(b.cancels, id)
		b.mu.Unlock()
	}()

	bc, err := b.store.GetBroadcast(id)
	if err != nil {
		log.Printf("[broadcast %d] load failed: %v", id, err)
		return
	}

	// Mark running.
	now := time.Now().UTC()
	bc.Status = "running"
	bc.StartedAt = &now
	if err := b.store.UpdateBroadcastStatus(bc); err != nil {
		log.Printf("[broadcast %d] mark running failed: %v", id, err)
	}

	recipients, err := b.store.ListBroadcastRecipients(id, "pending")
	if err != nil {
		log.Printf("[broadcast %d] load recipients failed: %v", id, err)
		b.markFinished(id, "failed")
		return
	}
	if len(recipients) == 0 {
		b.markFinished(id, "completed")
		return
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	if bc.ShuffleOrder {
		rng.Shuffle(len(recipients), func(i, j int) {
			recipients[i], recipients[j] = recipients[j], recipients[i]
		})
	}

	log.Printf("[broadcast %d] starting with %d recipients (delay %d-%dms, batch=%d)",
		id, len(recipients), bc.DelayMinMs, bc.DelayMaxMs, bc.BatchSize)

	for i, r := range recipients {
		select {
		case <-ctx.Done():
			log.Printf("[broadcast %d] cancelled at index %d", id, i)
			b.markFinished(id, "cancelled")
			return
		default:
		}

		// Pilih variant kalau body pakai spintax
		message := pickVariant(bc.Message, rng)

		resp, sendErr := b.sendOne(bc, r.Recipient, message)
		sentAt := time.Now().UTC()

		if sendErr != nil {
			errMsg := sendErr.Error()
			_ = b.store.UpdateBroadcastRecipient(r.ID, "failed", errMsg, "", &sentAt)
			_ = b.store.IncrementBroadcastCounter(id, "failed")
			log.Printf("[broadcast %d] %d/%d FAIL %s: %v", id, i+1, len(recipients), r.Recipient, sendErr)
		} else {
			respStr := ""
			if resp != nil {
				if bb, mErr := json.Marshal(resp); mErr == nil {
					respStr = string(bb)
				}
			}
			_ = b.store.UpdateBroadcastRecipient(r.ID, "sent", "", respStr, &sentAt)
			_ = b.store.IncrementBroadcastCounter(id, "sent")
			log.Printf("[broadcast %d] %d/%d OK %s", id, i+1, len(recipients), r.Recipient)
		}

		// Skip delay setelah pesan terakhir.
		if i >= len(recipients)-1 {
			continue
		}

		// Batch break setelah tiap N pesan kalau aktif.
		if bc.BatchSize > 0 && (i+1)%bc.BatchSize == 0 {
			d := randDuration(rng, bc.BatchPauseMinMs, bc.BatchPauseMaxMs)
			log.Printf("[broadcast %d] batch break after %d (sleeping %v)", id, i+1, d)
			if !sleep(ctx, d) {
				b.markFinished(id, "cancelled")
				return
			}
			continue
		}

		// Random delay biasa.
		d := randDuration(rng, bc.DelayMinMs, bc.DelayMaxMs)
		if !sleep(ctx, d) {
			b.markFinished(id, "cancelled")
			return
		}
	}

	b.markFinished(id, "completed")
}

func (b *Broadcaster) sendOne(bc *store.Broadcast, recipient, message string) (*wa.Response, error) {
	switch strings.ToLower(bc.MessageType) {
	case "text":
		return b.wa.SendText(bc.DeviceID, wa.SendTextRequest{
			Phone:   recipient,
			Message: message,
		})
	case "image", "video", "file", "document", "audio":
		if bc.MediaURL == "" {
			return nil, fmt.Errorf("media_url required for type %s", bc.MessageType)
		}
		return b.wa.SendMediaURL(bc.DeviceID, bc.MessageType, recipient, bc.MediaURL, message)
	}
	return nil, fmt.Errorf("unsupported message_type %q", bc.MessageType)
}

func (b *Broadcaster) markFinished(id int64, status string) {
	now := time.Now().UTC()
	if err := b.store.MarkBroadcastFinished(id, status, &now); err != nil {
		log.Printf("[broadcast %d] mark finished failed: %v", id, err)
	}
}

// --- spintax & helpers ----------------------------------------------------

// spintaxRE match {a|b|c} (non-nested di satu pass; nested groups di-resolve
// via loop di pickVariant).
var spintaxRE = regexp.MustCompile(`\{([^{}]+)\}`)

// pickVariant: pilih satu varian random dari setiap {a|b|c} di body.
// Mendukung nested via repeated apply sampai tidak ada lagi yg match.
// Contoh: "Halo {kak|bro|sis} {pagi|siang}" → "Halo bro pagi"
func pickVariant(s string, rng *rand.Rand) string {
	if !strings.Contains(s, "{") {
		return s
	}
	// Loop terbatas 10x untuk hindari infinite di edge case (mis. nested).
	for k := 0; k < 10; k++ {
		next := spintaxRE.ReplaceAllStringFunc(s, func(m string) string {
			inner := m[1 : len(m)-1]
			opts := strings.Split(inner, "|")
			return opts[rng.Intn(len(opts))]
		})
		if next == s {
			break
		}
		s = next
	}
	return s
}

func randDuration(rng *rand.Rand, minMs, maxMs int) time.Duration {
	if minMs < 0 {
		minMs = 0
	}
	if maxMs < minMs {
		maxMs = minMs
	}
	if maxMs == 0 {
		return 0
	}
	n := minMs + rng.Intn(maxMs-minMs+1)
	return time.Duration(n) * time.Millisecond
}

// sleep returns false kalau context cancelled before timer.
func sleep(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// --- Phone number parser & normalizer (di-package level supaya bisa
// di-test sebagai unit dan dipakai dari handler) ---------------------------

var (
	splitRecipientsRE = regexp.MustCompile(`[\s,;|]+`)
	onlyDigitsRE      = regexp.MustCompile(`\D`)
)

// ParseRecipients menerima input bebas (comma / newline / semicolon / spasi
// sebagai pemisah), normalisasi ke JID WhatsApp, dedupe, dan return list.
//
// Aturan normalisasi:
//   - Format "08xxx" → "62xxx" (default Indonesia; sederhana, tidak coba
//     deteksi negara lain. User dari negara lain langsung ketik kode negara)
//   - "+62xxx" → "62xxx" (strip plus)
//   - Sudah ada "@" → pakai as-is (asumsi JID lengkap mis. xxxxx@g.us untuk grup)
//   - 8-15 digit tanpa "@" → append "@s.whatsapp.net"
//   - Lain-lain → masuk skipped list dengan reason
//
// Return: list valid recipients, list raw input yang invalid (utk UI feedback).
func ParseRecipients(raw string) (valid []store.BroadcastRecipient, invalid []string) {
	seen := map[string]bool{}
	for _, tok := range splitRecipientsRE.Split(raw, -1) {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		jid, ok := NormalizeRecipient(tok)
		if !ok {
			invalid = append(invalid, tok)
			continue
		}
		if seen[jid] {
			continue
		}
		seen[jid] = true
		valid = append(valid, store.BroadcastRecipient{
			Recipient: jid,
			RawInput:  tok,
		})
	}
	return
}

// NormalizeRecipient melakukan normalisasi satu token. Return JID + ok flag.
func NormalizeRecipient(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	// Already a JID (DM atau group) — pakai apa adanya.
	if strings.Contains(s, "@") {
		// Sanity: harus ada karakter selain @
		if len(s) < 4 {
			return "", false
		}
		return s, true
	}
	// Strip karakter non-digit (handle +62, 62-812-xxx, (62) 812 xxx, dll)
	digits := onlyDigitsRE.ReplaceAllString(s, "")
	if digits == "" {
		return "", false
	}
	// 0xxx → 62xxx (Indonesia default)
	if strings.HasPrefix(digits, "0") {
		digits = "62" + digits[1:]
	}
	// Validasi panjang minimal 8 (negara terkecil ~7-8 digit setelah country code)
	if len(digits) < 8 || len(digits) > 15 {
		return "", false
	}
	return digits + "@s.whatsapp.net", true
}
