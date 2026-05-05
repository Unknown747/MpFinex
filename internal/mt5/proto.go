package mt5

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"net"
	"time"
)

// MT5 binary protocol command codes (little-endian uint16 in packet header).
// Based on reverse-engineering of the MetaQuotes Trade Server binary protocol.
const (
	CmdHello    uint16 = 0x0001 // Server→Client: version greeting
	CmdAuthA    uint16 = 0x0002 // Client→Server: SRP-6a login + A value
	CmdAuthB    uint16 = 0x0003 // Server→Client: SRP-6a salt + B value
	CmdAuthM    uint16 = 0x0004 // Client→Server: SRP-6a M1 proof
	CmdAuthOK   uint16 = 0x0005 // Server→Client: M2 proof + account session
	CmdAuthFail uint16 = 0x0006 // Server→Client: auth failure code
	CmdAccInfo  uint16 = 0x0010 // Server→Client: account info push
	CmdPing     uint16 = 0x0040 // Keepalive ping
)

// writePacket encodes and writes one MT5 binary packet.
// Wire format: [uint16 LE cmd][uint32 LE body_size][body bytes]
func writePacket(conn net.Conn, cmd uint16, body []byte) error {
	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	header := make([]byte, 6)
	binary.LittleEndian.PutUint16(header[0:2], cmd)
	binary.LittleEndian.PutUint32(header[2:6], uint32(len(body)))
	buf := append(header, body...)
	_, err := conn.Write(buf)
	return err
}

// readPacket reads one MT5 binary packet from conn, with the given read timeout.
func readPacket(conn net.Conn, timeout time.Duration) (cmd uint16, body []byte, err error) {
	conn.SetReadDeadline(time.Now().Add(timeout))
	hdr := make([]byte, 6)
	if _, err = io.ReadFull(conn, hdr); err != nil {
		return 0, nil, fmt.Errorf("header: %w", err)
	}
	cmd = binary.LittleEndian.Uint16(hdr[0:2])
	size := binary.LittleEndian.Uint32(hdr[2:6])
	if size > 4<<20 {
		return 0, nil, fmt.Errorf("packet too large: %d bytes (cmd=0x%04X)", size, cmd)
	}
	body = make([]byte, size)
	if size > 0 {
		if _, err = io.ReadFull(conn, body); err != nil {
			return 0, nil, fmt.Errorf("body (cmd=0x%04X size=%d): %w", cmd, size, err)
		}
	}
	return cmd, body, nil
}

// ─── Small helpers for binary parsing ────────────────────────────────────────

func getString(b []byte, off int) (string, int) {
	end := off
	for end < len(b) && b[end] != 0 {
		end++
	}
	s := string(b[off:end])
	if end < len(b) {
		end++
	}
	return s, end
}

func getFloat64LE(b []byte, off int) (float64, int) {
	if off+8 > len(b) {
		return 0, off
	}
	bits := binary.LittleEndian.Uint64(b[off : off+8])
	return math.Float64frombits(bits), off + 8
}

func getUint32LE(b []byte, off int) (uint32, int) {
	if off+4 > len(b) {
		return 0, off
	}
	return binary.LittleEndian.Uint32(b[off : off+4]), off + 4
}

func getInt64LE(b []byte, off int) (int64, int) {
	if off+8 > len(b) {
		return 0, off
	}
	return int64(binary.LittleEndian.Uint64(b[off : off+8])), off + 8
}
