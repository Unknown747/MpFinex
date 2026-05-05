package mt5

import (
	"fmt"
	"time"
)

// StartHeartbeat memulai goroutine watchdog yang memantau koneksi MT5.
//
// Setiap interval:
//   - Kirim CmdPing ke server.
//   - Jika 2 kali berturut-turut gagal → reconnect dengan exponential backoff.
//
// Backoff reconnect: 1s → 2s → 4s → 8s, maksimal 3 kali percobaan.
// Jika reconnect gagal setelah 3x, log "MT5 disconnected permanently" dan
// panggil onPermanentFailure (menutup semua posisi + shutdown TUI).
func StartHeartbeat(client *Client, interval time.Duration, onPermanentFailure func()) {
	go func() {
		consecutiveFails := 0

		for {
			time.Sleep(interval)

			err := client.Ping()
			if err == nil {
				consecutiveFails = 0
				continue
			}

			consecutiveFails++
			client.log(fmt.Sprintf("! Heartbeat ping gagal (%d/2): %v", consecutiveFails, err))

			if consecutiveFails < 2 {
				continue
			}

			// 2 kali gagal berturut-turut — mulai reconnect
			consecutiveFails = 0
			client.log("! Koneksi MT5 terputus — memulai reconnect...")

			if reconnected := attemptReconnect(client); !reconnected {
				// Semua percobaan reconnect habis
				client.log("✗ MT5 disconnected permanently — menutup semua posisi dan shutdown")
				onPermanentFailure()
				return
			}

			client.log("✓ Reconnect berhasil")
		}
	}()
}

// attemptReconnect mencoba reconnect dengan exponential backoff.
// Backoff: 1s, 2s, 4s, 8s — maksimal 3 kali percobaan.
// Return true jika berhasil, false jika semua percobaan gagal.
func attemptReconnect(client *Client) bool {
	const maxAttempts = 3
	backoff := time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		client.log(fmt.Sprintf("→ Reconnect attempt %d/%d (backoff=%s)...", attempt, maxAttempts, backoff))
		time.Sleep(backoff)

		client.Connect()
		if client.Status == StatusConnected {
			return true
		}

		client.log(fmt.Sprintf("✗ Reconnect %d gagal: %s", attempt, client.ErrMsg))
		backoff *= 2 // 1s → 2s → 4s → 8s
	}

	return false
}
