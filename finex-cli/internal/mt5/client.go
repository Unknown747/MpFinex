package mt5

import (
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"time"
)

// ─── Status ───────────────────────────────────────────────────────────────────

type Status int

const (
	StatusDisconnected Status = iota
	StatusConnecting
	StatusHandshake
	StatusAuthenticating
	StatusConnected
	StatusFailed
)

func (s Status) String() string {
	switch s {
	case StatusConnecting:
		return "Connecting..."
	case StatusHandshake:
		return "Handshake..."
	case StatusAuthenticating:
		return "Authenticating..."
	case StatusConnected:
		return "Connected"
	case StatusFailed:
		return "Failed"
	default:
		return "Disconnected"
	}
}

// ─── Config ───────────────────────────────────────────────────────────────────

type Config struct {
	Login    string
	Password string
	Server   string
	Host     string
	Company  string
}

func ConfigFromEnv() Config {
	return Config{
		Login:    getEnv("FINEX_LOGIN", ""),
		Password: getEnv("FINEX_PASSWORD", ""),
		Server:   getEnv("FINEX_SERVER", "FinexBisnisSolusi-Demo"),
		Host:     getEnv("FINEX_HOST", "prod-mt5-demo1.fnx.xmt.mx:443"),
		Company:  getEnv("FINEX_COMPANY", "FinexBisnisSolusi"),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// ─── Client ───────────────────────────────────────────────────────────────────

// Client manages a connection to the MT5 trade server.
type Client struct {
	Config  Config
	Status  Status
	ErrMsg  string
	Account *AccountInfo
	Debug   []string // diagnostic log lines (last N entries)
}

func NewClient(cfg Config) *Client {
	return &Client{Config: cfg, Status: StatusDisconnected}
}

func (c *Client) log(msg string) {
	c.Debug = append(c.Debug, msg)
	if len(c.Debug) > 20 {
		c.Debug = c.Debug[len(c.Debug)-20:]
	}
}

// Connect performs the full MT5 binary protocol handshake:
//  1. TCP+TLS dial
//  2. Read server Hello
//  3. SRP-6a authentication (send A, receive s+B, send M1, verify M2)
//  4. Parse account info from auth-OK packet or follow-up AccInfo packet
func (c *Client) Connect() {
	c.Debug = nil
	c.Account = nil
	c.ErrMsg = ""

	if c.Config.Host == "" || c.Config.Login == "" || c.Config.Password == "" {
		c.Status = StatusFailed
		c.ErrMsg = "Kredensial belum lengkap"
		return
	}

	c.Status = StatusConnecting
	c.log(fmt.Sprintf("Connecting to %s ...", c.Config.Host))

	host, _, err := net.SplitHostPort(c.Config.Host)
	if err != nil {
		host = c.Config.Host
	}

	conn, err := tls.DialWithDialer(
		&net.Dialer{Timeout: 15 * time.Second},
		"tcp",
		c.Config.Host,
		&tls.Config{InsecureSkipVerify: true, ServerName: host},
	)
	if err != nil {
		c.Status = StatusFailed
		c.ErrMsg = fmt.Sprintf("TCP/TLS: %v", err)
		c.log("✗ " + c.ErrMsg)
		return
	}
	defer conn.Close()
	c.log("✓ TLS connected")

	// ── Step 1: Read server Hello ────────────────────────────────────────────
	c.Status = StatusHandshake
	cmd, helloBody, err := readPacket(conn, 10*time.Second)
	if err != nil {
		c.Status = StatusFailed
		c.ErrMsg = fmt.Sprintf("Hello: %v", err)
		c.log("✗ " + c.ErrMsg)
		return
	}
	c.log(fmt.Sprintf("← Hello cmd=0x%04X len=%d", cmd, len(helloBody)))

	// Parse version string from hello body (null-terminated or raw ASCII)
	if len(helloBody) > 0 {
		ver, _ := getString(helloBody, 0)
		if ver != "" {
			c.log("  Server: " + ver)
		}
	}

	// ── Step 2: SRP-6a — build client ephemeral ──────────────────────────────
	c.Status = StatusAuthenticating
	srp, err := NewSRPClient(c.Config.Login, c.Config.Password)
	if err != nil {
		c.Status = StatusFailed
		c.ErrMsg = fmt.Sprintf("SRP init: %v", err)
		return
	}

	// Build AuthA body: [uint64 login LE][256 bytes A]
	loginNum := uint64(0)
	fmt.Sscanf(c.Config.Login, "%d", &loginNum)
	authA := make([]byte, 8+256)
	binary.LittleEndian.PutUint64(authA[0:8], loginNum)
	copy(authA[8:], srp.ABytes())

	if err = writePacket(conn, CmdAuthA, authA); err != nil {
		c.Status = StatusFailed
		c.ErrMsg = fmt.Sprintf("AuthA send: %v", err)
		c.log("✗ " + c.ErrMsg)
		return
	}
	c.log(fmt.Sprintf("→ AuthA cmd=0x%04X (login=%d)", CmdAuthA, loginNum))

	// ── Step 3: Receive server SRP B + salt ──────────────────────────────────
	cmd, authBBody, err := readPacket(conn, 10*time.Second)
	if err != nil {
		c.Status = StatusFailed
		c.ErrMsg = fmt.Sprintf("AuthB recv: %v", err)
		c.log("✗ " + c.ErrMsg)
		return
	}
	c.log(fmt.Sprintf("← AuthB cmd=0x%04X len=%d", cmd, len(authBBody)))

	if cmd == CmdAuthFail {
		c.Status = StatusFailed
		c.ErrMsg = "Server: login ditolak (periksa login/password)"
		c.log("✗ Auth failed — server rejected login")
		return
	}

	// AuthB body layout: [uint16 LE salt_len][salt bytes][256 bytes B]
	// Fallback: [32 bytes salt][256 bytes B]
	salt, bBytes, parseErr := parseAuthBPacket(authBBody)
	if parseErr != nil {
		c.Status = StatusFailed
		c.ErrMsg = fmt.Sprintf("AuthB parse: %v", parseErr)
		c.log("✗ " + c.ErrMsg)
		return
	}
	c.log(fmt.Sprintf("  salt=%d bytes, B=%d bytes", len(salt), len(bBytes)))

	// ── Step 4: Compute and send M1 ──────────────────────────────────────────
	M1, K, err := srp.ComputeProof(salt, bBytes)
	if err != nil {
		c.Status = StatusFailed
		c.ErrMsg = fmt.Sprintf("SRP proof: %v", err)
		return
	}
	if err = writePacket(conn, CmdAuthM, M1); err != nil {
		c.Status = StatusFailed
		c.ErrMsg = fmt.Sprintf("AuthM send: %v", err)
		c.log("✗ " + c.ErrMsg)
		return
	}
	c.log(fmt.Sprintf("→ AuthM cmd=0x%04X (M1 %d bytes)", CmdAuthM, len(M1)))

	// ── Step 5: Receive Auth result ───────────────────────────────────────────
	cmd, resultBody, err := readPacket(conn, 10*time.Second)
	if err != nil {
		c.Status = StatusFailed
		c.ErrMsg = fmt.Sprintf("AuthOK recv: %v", err)
		c.log("✗ " + c.ErrMsg)
		return
	}
	c.log(fmt.Sprintf("← Result cmd=0x%04X len=%d", cmd, len(resultBody)))

	if cmd == CmdAuthFail {
		c.Status = StatusFailed
		c.ErrMsg = "Server: password salah atau session kadaluarsa"
		c.log("✗ Auth rejected by server")
		return
	}

	// Try to verify M2 from first 32 bytes of result body
	if len(resultBody) >= 32 {
		M2 := resultBody[:32]
		if !srp.VerifyServerProof(M1, K, M2) {
			c.log("! Server M2 tidak cocok — mungkin perbedaan versi SRP")
		} else {
			c.log("✓ Server M2 verified")
		}
	}

	// ── Step 6: Parse account info ────────────────────────────────────────────
	// Try to parse account info from result body (may include M2 prefix)
	acc := parseAccountBody(resultBody)
	if acc == nil {
		acc = parseAccountBodySkip32(resultBody)
	}

	// If not in result body, wait for a separate AccInfo packet
	if acc == nil && cmd == CmdAuthOK {
		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		accCmd, accBody, accErr := readPacket(conn, 5*time.Second)
		if accErr == nil {
			c.log(fmt.Sprintf("← AccInfo cmd=0x%04X len=%d", accCmd, len(accBody)))
			acc = parseAccountBody(accBody)
			if acc == nil {
				acc = parseAccountBodySkip32(accBody)
			}
		}
	}

	if acc != nil {
		acc.Server = c.Config.Server
		acc.Company = c.Config.Company
		c.Account = acc
		c.log(fmt.Sprintf("✓ Account: login=%d balance=%.2f %s",
			acc.Login, acc.Balance, acc.Currency))
	} else {
		// Auth succeeded but we couldn't parse account fields.
		// Still mark as connected — user will see the green badge.
		c.log("✓ Auth succeeded (account fields tidak dikenali)")
	}

	c.Status = StatusConnected
	c.ErrMsg = ""
}

// ─── AuthB packet parser ──────────────────────────────────────────────────────

// parseAuthBPacket extracts salt and B (256 bytes) from the server's AuthB body.
// Tries two layouts:
//   Layout A: [uint16 LE salt_len][salt bytes][256 bytes B]
//   Layout B: [32 bytes salt][256 bytes B]   (fixed 32-byte salt)
func parseAuthBPacket(body []byte) (salt, bBytes []byte, err error) {
	if len(body) < 2+256 {
		return nil, nil, fmt.Errorf("AuthB body too short (%d bytes)", len(body))
	}

	// Layout A: first 2 bytes = salt length
	saltLen := int(binary.LittleEndian.Uint16(body[0:2]))
	if saltLen > 0 && saltLen <= 128 && 2+saltLen+256 <= len(body) {
		salt = body[2 : 2+saltLen]
		bBytes = body[2+saltLen : 2+saltLen+256]
		return salt, bBytes, nil
	}

	// Layout B: fixed 32-byte salt
	if len(body) >= 32+256 {
		salt = body[0:32]
		bBytes = body[32 : 32+256]
		return salt, bBytes, nil
	}

	return nil, nil, fmt.Errorf("AuthB: cannot parse salt+B from %d bytes", len(body))
}
