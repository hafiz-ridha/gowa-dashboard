#!/bin/sh
set -e

# Bind mounts are often root-owned on the host; the app runs as gowauser and SQLite
# needs write access (DB + WAL). Fix ownership at start (requires container root).
for d in /app/storages /app/statics /app/statics/qrcode /app/statics/senditems /app/statics/media; do
	[ -d "$d" ] || mkdir -p "$d"
	chown -R gowauser:gowa "$d" 2>/dev/null || true
done

# Default AI_REPLY_ENABLED to "true" supaya dashboard's /aireply/* tab works
# out-of-the-box. User dapat override eksplisit lewat src/.env atau
# compose environment kalau memang mau matikan.
: "${AI_REPLY_ENABLED:=true}"
export AI_REPLY_ENABLED

# --- AI_ENCRYPTION_KEY priority -----------------------------------------
#
# Priority (highest first):
#
#   1. KEYFILE (/app/storages/.ai-encryption-key)
#      Database / persistent storage. Ini yang dipakai untuk encrypt API
#      key provider yang TERSIMPAN di SQLite. Kalau di-override, data
#      lama jadi unreadable. Karena itu keyfile selalu menang.
#
#   2. ENV VAR (AI_ENCRYPTION_KEY dari src/.env atau compose)
#      Cuma dipakai sebagai BOOTSTRAP — kalau keyfile belum ada, env var
#      jadi sumber awal dan otomatis di-copy ke keyfile.
#
#   3. GENERATE BARU (terakhir, fallback aman)
#      Kalau keduanya kosong/invalid, generate 64-char hex random.
#
# Konsekuensi praktis:
#   - User set AI_ENCRYPTION_KEY=<hex> di src/.env, first boot → env value
#     dipakai DAN disimpan ke keyfile.
#   - User ganti AI_ENCRYPTION_KEY di src/.env di boot berikutnya →
#     DIABAIKAN (keyfile menang). Warning di log. Untuk rotate key:
#     stop container, hapus keyfile, restart.
#   - User set AI_ENCRYPTION_KEY=passphrase-bukan-hex → warning di log,
#     diabaikan, lanjut ke keyfile / generate.
#
# AES-GCM butuh exactly 32 byte → 64 char hex (0-9a-f).

# is_valid_hex_key: pure POSIX shell, no grep/awk dependency.
is_valid_hex_key() {
	_k="$1"
	[ "${#_k}" = "64" ] || return 1
	case "$_k" in
		*[!0-9a-fA-F]*) return 1 ;;
		*) return 0 ;;
	esac
}

trim_key() {
	printf '%s' "$1" | tr -d ' \t\n\r'
}

# Generate random hex key — bulletproof terhadap variasi `od` di BusyBox/GNU.
# `tr -dc 'a-f0-9'` strips semua non-hex, `cut` ambil 64 char pertama.
generate_hex_key() {
	dd if=/dev/urandom bs=64 count=1 2>/dev/null \
		| od -An -tx1 \
		| tr -dc 'a-f0-9' \
		| cut -c 1-64
}

write_keyfile() {
	printf "%s" "$1" > "${KEYFILE}"
	chmod 600 "${KEYFILE}" 2>/dev/null || true
	chown gowauser:gowa "${KEYFILE}" 2>/dev/null || true
}

if [ "${AI_REPLY_ENABLED}" = "true" ]; then
	KEYFILE="/app/storages/.ai-encryption-key"

	# Snapshot kedua sumber dulu, baru putuskan mana yg dipakai.
	# Ini supaya kita bisa kasih warning yg akurat kalau keduanya
	# diset tapi berbeda (kasus user salah rotate key).
	ENV_KEY=""
	FILE_KEY=""

	if [ -n "${AI_ENCRYPTION_KEY:-}" ]; then
		_tmp=$(trim_key "$AI_ENCRYPTION_KEY")
		if is_valid_hex_key "$_tmp"; then
			ENV_KEY="$_tmp"
		else
			echo "[entrypoint] WARNING: AI_ENCRYPTION_KEY env var bukan valid 64-char hex"
			echo "[entrypoint]          (length: ${#_tmp}, expected: 64; valid chars: 0-9a-f only)"
			echo "[entrypoint]          Ignored. Pakai keyfile (kalau ada) atau generate baru."
			echo "[entrypoint]          Hapus AI_ENCRYPTION_KEY di src/.env supaya warning ini hilang,"
			echo "[entrypoint]          atau set dengan: openssl rand -hex 32"
		fi
	fi

	if [ -f "${KEYFILE}" ]; then
		_tmp=$(trim_key "$(cat "${KEYFILE}" 2>/dev/null || true)")
		if is_valid_hex_key "$_tmp"; then
			FILE_KEY="$_tmp"
		else
			echo "[entrypoint] WARNING: ${KEYFILE} content invalid (${#_tmp} chars)"
			echo "[entrypoint]          Akan regenerate. Backup file ini dulu kalau penting!"
		fi
	fi

	# Decision matrix — keyfile > env > generate.
	KEY_SOURCE=""
	if [ -n "$FILE_KEY" ]; then
		AI_ENCRYPTION_KEY="$FILE_KEY"
		KEY_SOURCE="keyfile"
		if [ -n "$ENV_KEY" ] && [ "$FILE_KEY" != "$ENV_KEY" ]; then
			echo "[entrypoint] NOTE: AI_ENCRYPTION_KEY env var ≠ keyfile content."
			echo "[entrypoint]       Pakai keyfile (otoritatif untuk data terenkripsi yg sudah ada)."
			echo "[entrypoint]       Mau rotate key ke env value? stop container → 'rm ${KEYFILE}' → restart."
			echo "[entrypoint]       (Konsekuensi: API key provider yg sudah tersimpan jadi unreadable,"
			echo "[entrypoint]        user harus input ulang via dashboard AI Reply tab.)"
		fi
	elif [ -n "$ENV_KEY" ]; then
		AI_ENCRYPTION_KEY="$ENV_KEY"
		KEY_SOURCE="env (bootstrapped to keyfile)"
		write_keyfile "$ENV_KEY"
		echo "[entrypoint] Bootstrapped ${KEYFILE} from env var"
		echo "[entrypoint] !! BACKUP THIS FILE !! Mulai sekarang keyfile = source of truth."
	else
		AI_ENCRYPTION_KEY=$(generate_hex_key)
		if ! is_valid_hex_key "$AI_ENCRYPTION_KEY"; then
			echo "[entrypoint] FATAL: failed to generate valid 64-char hex key"
			echo "[entrypoint]        generated length: ${#AI_ENCRYPTION_KEY}"
			exit 1
		fi
		write_keyfile "$AI_ENCRYPTION_KEY"
		KEY_SOURCE="generated"
		echo "[entrypoint] Generated new AI_ENCRYPTION_KEY -> ${KEYFILE}"
		echo "[entrypoint] !! BACKUP THIS FILE !! Losing it makes all stored provider API keys unreadable."
	fi

	# Print non-sensitive summary untuk verifikasi
	_preview=$(printf '%s' "$AI_ENCRYPTION_KEY" | cut -c 1-4)"…"$(printf '%s' "$AI_ENCRYPTION_KEY" | cut -c 61-64)
	echo "[entrypoint] AI_ENCRYPTION_KEY: ${_preview} (64 hex chars, source: ${KEY_SOURCE})"

	export AI_ENCRYPTION_KEY
fi

exec su-exec gowauser /app/whatsapp "$@"
