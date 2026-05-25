package api

import (
	"bytes"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/xuri/excelize/v2"
)

// Excel export / import untuk tab Jadwal & Reminder.
//
// Export: GET /api/schedules/export.xlsx -> file .xlsx (Schedules sheet) berisi
// SEMUA jadwal, satu baris per row. Kolom mengikuti urutan di xlsxHeaders.
// Field read-only (last_run_at, next_run_at, last_status, run_count) ikut
// di-export untuk arsip / laporan tapi diabaikan saat import.
//
// Import: POST /api/schedules/import (multipart "file") -> parse xlsx, validate
// per-baris pakai h.buildSchedule (re-use validasi yg sama dgn endpoint
// create/update), skip baris invalid + lanjut ke baris berikutnya. Response
// adalah ringkasan per-baris {row, name, ok, schedule_id, error}.

const xlsxSheetName = "Schedules"

// xlsxHeaders define kolom export. Import bersifat lenient — mencocokkan
// header berdasar nama (case-insensitive) sehingga user boleh hapus kolom
// read-only atau menambah kolom catatan tanpa break parser.
var xlsxHeaders = []string{
	"id",            // export-only (diabaikan saat import — insert selalu jadi row baru)
	"name",          // required
	"device_id",     //
	"recipient",     // required
	"message_type",  // required (text|image|video|file|audio|location|link)
	"message",       //
	"media_url",     //
	"caption",       //
	"latitude",      //
	"longitude",     //
	"link_url",      //
	"schedule_type", // required (once|daily|weekly|monthly|yearly|cron)
	"run_at",        // format "YYYY-MM-DD HH:MM:SS" di timezone schedule
	"cron_expr",     // untuk schedule_type=cron, atau CSV days-of-week (0-6) untuk weekly
	"timezone",      // mis. Asia/Jakarta
	"enabled",       // true|false (default true kalau kosong)
	// --- read-only di bawah ini (export only) ---
	"last_run_at",
	"next_run_at",
	"last_status",
	"run_count",
}

// exportSchedulesXLSX -> file .xlsx download. Tidak ada filter di server side;
// frontend yang tentukan visibility (mis. search/sort tinggal di client) tapi
// export selalu full untuk kepentingan backup.
func (h *Handlers) exportSchedulesXLSX(c *fiber.Ctx) error {
	list, err := h.Store.ListSchedules()
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	// excelize membuat "Sheet1" default. Kita buat sheet "Schedules" baru lalu
	// hapus Sheet1 supaya file rapi dan import bisa pakai sheet name sebagai
	// preferred match.
	idx, err := f.NewSheet(xlsxSheetName)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "create sheet: " + err.Error()})
	}
	f.SetActiveSheet(idx)
	_ = f.DeleteSheet("Sheet1")

	// Header row.
	for i, name := range xlsxHeaders {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(xlsxSheetName, cell, name)
	}
	// Style header — bold + light-gray fill supaya gampang dibedakan.
	if style, e := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true},
		Fill: excelize.Fill{Type: "pattern", Pattern: 1, Color: []string{"#E0E6ED"}},
	}); e == nil {
		endCol, _ := excelize.CoordinatesToCellName(len(xlsxHeaders), 1)
		_ = f.SetCellStyle(xlsxSheetName, "A1", endCol, style)
	}

	// Data rows. RunAt di-render di timezone schedule supaya user lihat jam
	// yang sama dengan UI (bukan UTC raw).
	for r, sc := range list {
		row := r + 2 // baris 2 dst (baris 1 = header)
		runAtStr := ""
		if sc.RunAt != nil {
			tz := sc.Timezone
			if tz == "" {
				tz = "Local"
			}
			loc, _ := time.LoadLocation(tz)
			if loc == nil {
				loc = time.Local
			}
			runAtStr = sc.RunAt.In(loc).Format("2006-01-02 15:04:05")
		}
		lastRunStr := ""
		if sc.LastRunAt != nil {
			lastRunStr = sc.LastRunAt.UTC().Format(time.RFC3339)
		}
		nextRunStr := ""
		if sc.NextRunAt != nil {
			nextRunStr = sc.NextRunAt.UTC().Format(time.RFC3339)
		}

		values := []any{
			sc.ID,                // id
			sc.Name,              // name
			sc.DeviceID,          // device_id
			sc.Recipient,         // recipient
			sc.MessageType,       // message_type
			sc.Message,           // message
			sc.MediaURL,          // media_url
			sc.Caption,           // caption
			sc.Latitude,          // latitude
			sc.Longitude,         // longitude
			sc.LinkURL,           // link_url
			sc.ScheduleType,      // schedule_type
			runAtStr,             // run_at
			sc.CronExpr,          // cron_expr
			sc.Timezone,          // timezone
			boolToXLSXStr(sc.Enabled), // enabled
			lastRunStr,           // last_run_at
			nextRunStr,           // next_run_at
			sc.LastStatus,        // last_status
			sc.RunCount,          // run_count
		}
		for i, v := range values {
			cell, _ := excelize.CoordinatesToCellName(i+1, row)
			_ = f.SetCellValue(xlsxSheetName, cell, v)
		}
	}

	// Lebar kolom yg lebih nyaman untuk dibaca. Tidak presisi — cuma supaya
	// kolom message & datetime tidak terlalu sempit saat dibuka di Excel.
	_ = f.SetColWidth(xlsxSheetName, "A", "A", 6)  // id
	_ = f.SetColWidth(xlsxSheetName, "B", "B", 22) // name
	_ = f.SetColWidth(xlsxSheetName, "C", "C", 14) // device_id
	_ = f.SetColWidth(xlsxSheetName, "D", "D", 28) // recipient
	_ = f.SetColWidth(xlsxSheetName, "F", "F", 40) // message
	_ = f.SetColWidth(xlsxSheetName, "M", "M", 22) // run_at
	_ = f.SetColWidth(xlsxSheetName, "N", "N", 18) // cron_expr
	_ = f.SetColWidth(xlsxSheetName, "O", "O", 16) // timezone

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "encode xlsx: " + err.Error()})
	}
	fname := fmt.Sprintf("schedules-%s.xlsx", time.Now().Format("20060102-150405"))
	c.Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Set("Content-Disposition", `attachment; filename="`+fname+`"`)
	c.Set("Cache-Control", "no-store")
	return c.Send(buf.Bytes())
}

// importResult — ringkasan per-baris dari file xlsx yang di-import. Dipakai
// frontend untuk tampilkan tabel "berhasil/gagal" supaya user tahu baris mana
// yang perlu diperbaiki di file sumber.
type importResult struct {
	Row        int    `json:"row"`                   // 1-based, sesuai nomor baris di Excel (header = 1)
	Name       string `json:"name,omitempty"`        // dari kolom name (kalau ada)
	OK         bool   `json:"ok"`                    //
	ScheduleID int64  `json:"schedule_id,omitempty"` // diisi kalau OK
	Error      string `json:"error,omitempty"`       // pesan validasi atau DB error
}

func (h *Handlers) importSchedulesXLSX(c *fiber.Ctx) error {
	fileHeader, err := c.FormFile("file")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "field 'file' wajib ada di multipart: " + err.Error()})
	}
	fp, err := fileHeader.Open()
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "open uploaded file: " + err.Error()})
	}
	defer fp.Close()

	xlsx, err := excelize.OpenReader(fp)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "file bukan .xlsx valid: " + err.Error()})
	}
	defer func() { _ = xlsx.Close() }()

	sheets := xlsx.GetSheetList()
	if len(sheets) == 0 {
		return c.Status(400).JSON(fiber.Map{"error": "xlsx tidak punya sheet apapun"})
	}
	// Prefer sheet "Schedules" kalau ada; kalau tidak, ambil sheet pertama.
	sheet := sheets[0]
	for _, s := range sheets {
		if strings.EqualFold(s, xlsxSheetName) {
			sheet = s
			break
		}
	}

	rows, err := xlsx.GetRows(sheet)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "baca rows: " + err.Error()})
	}
	if len(rows) < 2 {
		return c.Status(400).JSON(fiber.Map{"error": "xlsx harus punya baris header + minimal 1 baris data"})
	}

	// Build header → column-index map (case-insensitive + trim). Memungkinkan
	// user re-order kolom atau menghapus kolom read-only di file sumber.
	headerIdx := map[string]int{}
	for i, h := range rows[0] {
		key := strings.ToLower(strings.TrimSpace(h))
		if key != "" {
			headerIdx[key] = i
		}
	}
	// Validasi kolom wajib. Sisanya optional — buildSchedule yang validasi
	// kombinasi field per message_type / schedule_type.
	for _, req := range []string{"name", "recipient", "message_type", "schedule_type"} {
		if _, ok := headerIdx[req]; !ok {
			return c.Status(400).JSON(fiber.Map{
				"error": "kolom wajib tidak ditemukan di header: " + req +
					". Pastikan baris pertama berisi nama kolom (mis. download template via Export dulu).",
			})
		}
	}

	cell := func(row []string, header string) string {
		if i, ok := headerIdx[header]; ok && i < len(row) {
			return strings.TrimSpace(row[i])
		}
		return ""
	}

	results := make([]importResult, 0, len(rows)-1)
	okCount := 0
	failCount := 0

	for ri := 1; ri < len(rows); ri++ {
		row := rows[ri]
		excelRow := ri + 1 // user-facing row number (1-based, header = 1)

		// Skip baris yang full empty — biasanya gap di tengah / trailing.
		empty := true
		for _, c := range row {
			if strings.TrimSpace(c) != "" {
				empty = false
				break
			}
		}
		if empty {
			continue
		}

		req := &scheduleReq{
			Name:         cell(row, "name"),
			DeviceID:     cell(row, "device_id"),
			Recipient:    cell(row, "recipient"),
			MessageType:  cell(row, "message_type"),
			Message:      cell(row, "message"),
			MediaURL:     cell(row, "media_url"),
			Caption:      cell(row, "caption"),
			Latitude:     cell(row, "latitude"),
			Longitude:    cell(row, "longitude"),
			LinkURL:      cell(row, "link_url"),
			ScheduleType: cell(row, "schedule_type"),
			RunAt:        cell(row, "run_at"),
			CronExpr:     cell(row, "cron_expr"),
			Timezone:     cell(row, "timezone"),
		}
		// Enabled bersifat opsional — kalau kolom tidak ada atau cell kosong,
		// biarkan nil supaya buildSchedule pakai default (true utk insert).
		if v := cell(row, "enabled"); v != "" {
			b := parseBoolish(v)
			req.Enabled = &b
		}

		result := importResult{Row: excelRow, Name: req.Name}

		sc, err := h.buildSchedule(req, nil)
		if err != nil {
			result.OK = false
			result.Error = err.Error()
			failCount++
			results = append(results, result)
			continue
		}
		id, err := h.Store.CreateSchedule(sc)
		if err != nil {
			result.OK = false
			result.Error = "save: " + err.Error()
			failCount++
			results = append(results, result)
			continue
		}
		sc.ID = id
		if err := h.Scheduler.Reload(id); err != nil {
			// Tetap dihitung sukses (row sudah di DB), tapi catat error registrasi
			// supaya user tahu jadwal ini tidak akan fire sampai dashboard
			// restart / di-toggle ulang.
			result.OK = true
			result.ScheduleID = id
			result.Error = "tersimpan tapi gagal register ke scheduler: " + err.Error()
			okCount++
			results = append(results, result)
			log.Printf("[xlsx-import] row %d saved as id=%d but scheduler.Reload failed: %v", excelRow, id, err)
			continue
		}
		result.OK = true
		result.ScheduleID = id
		okCount++
		results = append(results, result)
	}

	log.Printf("[xlsx-import] done: %d imported, %d failed (total %d rows examined)",
		okCount, failCount, okCount+failCount)

	return c.JSON(fiber.Map{
		"imported_count": okCount,
		"failed_count":   failCount,
		"total":          okCount + failCount,
		"results":        results,
	})
}

// boolToXLSXStr — tulis "true"/"false" (lowercase) supaya konsisten dengan
// parseBoolish saat import dan tidak bingung dengan Excel TRUE/FALSE native.
func boolToXLSXStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// parseBoolish menerima beragam variasi yang user mungkin ketik di Excel:
// "true"/"false", "1"/"0", "yes"/"no", "y"/"n", "on"/"off", "aktif"/"tidak"
// (id). Default ke false hanya untuk nilai eksplisit-false; selain itu true.
func parseBoolish(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "", "0", "false", "no", "off", "tidak", "nonaktif", "n", "f":
		return false
	}
	return true
}
