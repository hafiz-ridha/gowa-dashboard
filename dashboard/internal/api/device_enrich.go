package api

import (
	"encoding/json"
	"log"
	"strings"
	"sync"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/dashboard/internal/wa"
)

// enrichDeviceListWithStatus mengambil hasil JSON dari /devices core, lalu
// untuk setiap device fire GET /devices/:id/status secara paralel untuk dapat
// is_connected & is_logged_in yang AKURAT (per-WS-state, bukan derived dari
// IsLoggedIn yang lossy). Setelah semua selesai, merge balik ke item asli.
//
// Return (newRaw, true) kalau enrichment sukses dan ada minimal satu device
// yang ter-augment. Return (raw, false) kalau parse list gagal — caller
// fallback ke raw asli.
//
// Filosofi: SEMUA logic ini hidup di dashboard supaya src/ tetap dekat ke
// upstream (sesuai CLAUDE.md guideline "Never modify src/ for things that
// can live in dashboard/").
func enrichDeviceListWithStatus(client *wa.Client, raw json.RawMessage) (json.RawMessage, bool) {
	if len(raw) == 0 {
		return raw, false
	}

	// Coba parse beberapa shape response dari core: direct array, wrapped
	// {data: [...]}, atau {devices: [...]}. Parser ini sengaja toleran karena
	// core history-nya pernah berubah shape & user mungkin pin versi lama.
	devs, wrapper := parseDeviceListPreservingShape(raw)
	if devs == nil {
		return raw, false
	}

	// Kumpulkan device IDs yang akan di-status-check. Pakai field "id" (alias),
	// itu kunci yang dikenali core's middleware ResolveDevice. Fallback ke
	// "device" / "name" / "jid" hanya kalau "id" kosong.
	ids := make([]string, len(devs))
	for i, d := range devs {
		ids[i] = pickDeviceIdentifier(d)
	}

	// Parallel fetch status. Per-device timeout sudah diatur di wa.Client.HTTP
	// (60s). Kita pakai goroutine per device tanpa worker pool karena jumlah
	// device biasanya kecil (<20); kalau user punya >>50 device, perlu refactor
	// pakai semaphore.
	type statusInfo struct {
		isConnected bool
		isLoggedIn  bool
		ok          bool // true kalau status call berhasil; kalau false, jangan override field existing
	}
	results := make([]statusInfo, len(devs))
	var wg sync.WaitGroup
	for i, id := range ids {
		if id == "" {
			continue
		}
		wg.Add(1)
		go func(idx int, deviceID string) {
			defer wg.Done()
			resp, err := client.DeviceStatus(deviceID)
			if err != nil || resp == nil || len(resp.Results) == 0 {
				// Silent — leave existing fields. Common case: device baru
				// dibuat & belum ada di DeviceManager → core return 404.
				return
			}
			var status map[string]any
			if err := json.Unmarshal(resp.Results, &status); err != nil {
				return
			}
			ci, ok1 := status["is_connected"].(bool)
			li, ok2 := status["is_logged_in"].(bool)
			if !ok1 && !ok2 {
				return
			}
			results[idx] = statusInfo{isConnected: ci, isLoggedIn: li, ok: true}
		}(i, id)
	}
	wg.Wait()

	// Merge results balik ke device map. Tambah field is_connected &
	// is_logged_in (overwrite kalau core kebetulan sudah kasih). Field "state"
	// JANGAN diubah supaya kalau ada code lain yg bergantung tetap aman —
	// dashboard frontend prefer is_connected/is_logged_in flags duluan dgn
	// state sebagai fallback.
	enrichedCount := 0
	for i, r := range results {
		if !r.ok {
			continue
		}
		devs[i]["is_connected"] = r.isConnected
		devs[i]["is_logged_in"] = r.isLoggedIn
		enrichedCount++
	}
	if enrichedCount == 0 {
		return raw, false
	}

	// Re-encode kembali ke shape asli (preserve wrapper kalau ada).
	encoded, err := encodeDeviceList(devs, wrapper)
	if err != nil {
		log.Printf("[enrich] re-encode failed, returning raw unchanged: %v", err)
		return raw, false
	}
	return encoded, true
}

// parseDeviceListPreservingShape memparsing raw JSON ke ([]map, wrapperKey).
// wrapperKey = "" untuk direct array, "data" / "devices" untuk wrapped.
func parseDeviceListPreservingShape(raw json.RawMessage) ([]map[string]any, string) {
	// Direct array.
	var direct []map[string]any
	if err := json.Unmarshal(raw, &direct); err == nil {
		return direct, ""
	}
	// {data: [...]}
	var w1 struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(raw, &w1); err == nil && w1.Data != nil {
		return w1.Data, "data"
	}
	// {devices: [...]}
	var w2 struct {
		Devices []map[string]any `json:"devices"`
	}
	if err := json.Unmarshal(raw, &w2); err == nil && w2.Devices != nil {
		return w2.Devices, "devices"
	}
	return nil, ""
}

func encodeDeviceList(devs []map[string]any, wrapperKey string) (json.RawMessage, error) {
	if wrapperKey == "" {
		return json.Marshal(devs)
	}
	return json.Marshal(map[string]any{wrapperKey: devs})
}

// pickDeviceIdentifier mengembalikan field yang dipakai sebagai X-Device-Id
// (key map di DeviceManager). Urutan preferensi sengaja sama dgn yang dipakai
// listLoggedInDeviceIDs supaya konsisten.
func pickDeviceIdentifier(d map[string]any) string {
	for _, k := range []string{"id", "device", "name", "jid"} {
		if v, ok := d[k].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
