package mt5

import (
	"encoding/binary"
	"math"
	"sync"
	"time"

	"github.com/finex/finex-cli/internal/market"
)

// PriceFeed command codes (extended MT5 binary protocol).
// CATATAN: Kode ini berdasarkan reverse-engineering umum MetaTrader 5 trade server.
// Verifikasi dengan broker masing-masing sebelum live trading — command codes
// bisa berbeda antar versi server.
const (
	CmdSubscribeTick uint16 = 0x0100 // Client→Server: subscribe tick feed per simbol
	CmdTickData      uint16 = 0x0101 // Server→Client: live tick price update
)

// TickPrice menyimpan satu update harga live dari MT5 trade server.
type TickPrice struct {
	Symbol string
	Bid    float64
	Ask    float64
	Mid    float64   // (Bid+Ask)/2 — digunakan oleh market simulator
	Time   time.Time
}

// PriceFeed mengelola langganan harga live ke MT5 trade server.
//
// Saat koneksi aktif, PriceFeed membaca paket CmdTickData secara terus-menerus
// dan memperbarui market.Market secara real-time. Jika koneksi terputus,
// PriceFeed berhenti dengan mulus sehingga market simulator dapat melanjutkan.
type PriceFeed struct {
	mu      sync.RWMutex
	latest  map[string]*TickPrice
	running bool
	stopCh  chan struct{}
}

// NewPriceFeed membuat PriceFeed baru, siap untuk di-Start().
func NewPriceFeed() *PriceFeed {
	return &PriceFeed{
		latest: make(map[string]*TickPrice),
		stopCh: make(chan struct{}),
	}
}

// Start memulai goroutine background yang subscribe ke MT5 dan memperbarui mkt.
// Non-blocking — langsung return. Panggil Stop() untuk menghentikan.
func (f *PriceFeed) Start(client *Client, mkt *market.Market, symbols []string) {
	f.mu.Lock()
	if f.running {
		f.mu.Unlock()
		return
	}
	f.running = true
	f.stopCh = make(chan struct{})
	f.mu.Unlock()

	go f.loop(client, mkt, symbols)
}

// Stop mengirim sinyal ke goroutine background untuk berhenti.
func (f *PriceFeed) Stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.running {
		close(f.stopCh)
		f.running = false
	}
}

// IsRunning mengembalikan true jika goroutine feed masih aktif.
func (f *PriceFeed) IsRunning() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.running
}

// GetLatest mengembalikan tick terbaru untuk simbol, atau nil jika belum tersedia.
func (f *PriceFeed) GetLatest(symbol string) *TickPrice {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.latest[symbol]
}

// loop adalah goroutine utama: kirim subscribe request lalu baca paket tick.
func (f *PriceFeed) loop(client *Client, mkt *market.Market, symbols []string) {
	defer func() {
		f.mu.Lock()
		f.running = false
		f.mu.Unlock()
	}()

	client.mu.Lock()
	conn := client.conn
	client.mu.Unlock()
	if conn == nil {
		return
	}

	// Kirim request subscribe untuk setiap simbol (body: null-terminated C-string)
	for _, sym := range symbols {
		body := append([]byte(sym), 0x00)
		if err := writePacket(conn, CmdSubscribeTick, body); err != nil {
			// Server tidak mendukung command ini — fallback ke simulator
			client.log("PriceFeed: subscribe gagal, fallback ke market simulator")
			return
		}
	}

	// Baca paket tick sampai dihentikan atau koneksi putus
	for {
		select {
		case <-f.stopCh:
			return
		default:
		}

		client.mu.Lock()
		conn = client.conn
		client.mu.Unlock()
		if conn == nil {
			return
		}

		cmd, body, err := readPacket(conn, 3*time.Second)
		if err != nil {
			// Timeout biasa — cek stop signal dan lanjut
			continue
		}
		if cmd != CmdTickData {
			continue // abaikan paket non-tick
		}

		tick := parseTickPacket(body)
		if tick == nil {
			continue
		}

		f.mu.Lock()
		f.latest[tick.Symbol] = tick
		f.mu.Unlock()

		// Perbarui market dengan mid-price sehingga bot menggunakan harga live
		mkt.UpdatePrice(tick.Symbol, tick.Mid)
	}
}

// parseTickPacket mendekode body dari paket CmdTickData.
//
// Layout yang diharapkan (little-endian):
//
//	[cstr    symbol ] — null-terminated
//	[float64 bid    ]
//	[float64 ask    ]
//	[uint64  time_ms] — milidetik sejak Unix epoch
func parseTickPacket(body []byte) *TickPrice {
	if len(body) < 20 {
		return nil
	}
	sym, off := getString(body, 0)
	if sym == "" || off >= len(body) {
		return nil
	}
	bid, off := getFloat64LE(body, off)
	ask, off := getFloat64LE(body, off)
	if off+8 > len(body) {
		return nil
	}
	msec := int64(binary.LittleEndian.Uint64(body[off : off+8]))

	if math.IsNaN(bid) || math.IsNaN(ask) || bid <= 0 || ask <= 0 {
		return nil
	}
	mid := (bid + ask) / 2
	return &TickPrice{
		Symbol: sym,
		Bid:    bid,
		Ask:    ask,
		Mid:    mid,
		Time:   time.UnixMilli(msec),
	}
}
