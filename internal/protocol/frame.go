package protocol

import (
	"encoding/binary"
	"fmt"
)

// Command tags (App → DTU request direction).
const (
	CmdAppInfoDataReq  uint16 = 0xA201 // response type; request uses 0xA301
	CmdAppInfoDataRes  uint16 = 0xA301
	CmdHeartbeat       uint16 = 0xA302
	CmdRealData        uint16 = 0xA303
	CmdCommand         uint16 = 0xA305
	CmdCommandStatus   uint16 = 0xA306
	CmdDevConfigFetch  uint16 = 0xA307
	CmdDevConfigPut    uint16 = 0xA308
	CmdGetConfig       uint16 = 0xA309
	CmdSetConfig       uint16 = 0xA310
	CmdRealDataNew     uint16 = 0xA311
	CmdAppGetHistPower uint16 = 0xA315
	CmdAppGetHistED    uint16 = 0xA316
	CmdESReg           uint16 = 0xC302
	CmdESData          uint16 = 0xC303
	CmdESUserSet       uint16 = 0xC308
	CmdGWInfo          uint16 = 0xDB01
	CmdGetConfigRes    uint16 = 0xDB08
	CmdSetConfigRes    uint16 = 0xDB07
	CmdGWNetInfo       uint16 = 0xDB06
)

var magic = [2]byte{'H', 'M'}

// notEncrypted tags never use AES-GCM.
func notEncrypted(tag uint16) bool {
	switch tag {
	case CmdAppInfoDataRes, CmdAppInfoDataReq, CmdHeartbeat:
		return true
	}
	return false
}

// Frame is a decoded HM frame.
type Frame struct {
	Tag      uint16
	Seq      uint16
	Extended bool
	// Extended-only metadata
	DTUSerial uint64
	Number    uint16
	Payload   []byte
}

// EncodeStandard builds a standard (optionally AES-GCM) frame.
func EncodeStandard(tag, seq uint16, payload []byte, encRand []byte, encrypted bool) []byte {
	body := payload
	encryptedOnWire := encrypted && !notEncrypted(tag) && len(encRand) == 16
	if encryptedOnWire {
		body, _ = Crypt(true, encRand, tag, seq, payload)
	}
	crc := CRC16(body)

	length := uint16(len(body) + 10)
	if encryptedOnWire {
		// length field counts ciphertext without GCM tag, plus 10-byte header
		// Python: struct.pack(">H", len(request_as_bytes) - 16 + 10)
		length = uint16(len(body) - 16 + 10)
	}

	out := make([]byte, 0, 10+len(body))
	out = append(out, magic[0], magic[1])
	var be [2]byte
	binary.BigEndian.PutUint16(be[:], tag)
	out = append(out, be[:]...)
	binary.BigEndian.PutUint16(be[:], seq)
	out = append(out, be[:]...)
	binary.BigEndian.PutUint16(be[:], crc)
	out = append(out, be[:]...)
	binary.BigEndian.PutUint16(be[:], length)
	out = append(out, be[:]...)
	out = append(out, body...)
	return out
}

// EncodeExtended builds an ES/gateway extended frame (never AES-GCM).
func EncodeExtended(tag, seq uint16, dtuSerial uint64, number uint16, payload []byte) []byte {
	crc := CRC16(payload)
	length := uint16(24 + len(payload))

	out := make([]byte, 0, 24+len(payload))
	out = append(out, magic[0], magic[1])
	var be [2]byte
	var be8 [8]byte
	binary.BigEndian.PutUint16(be[:], tag)
	out = append(out, be[:]...)
	binary.BigEndian.PutUint16(be[:], seq)
	out = append(out, be[:]...)
	binary.BigEndian.PutUint16(be[:], crc)
	out = append(out, be[:]...)
	binary.BigEndian.PutUint16(be[:], length)
	out = append(out, be[:]...)
	binary.BigEndian.PutUint16(be[:], 14)
	out = append(out, be[:]...)
	binary.BigEndian.PutUint64(be8[:], dtuSerial)
	out = append(out, be8[:]...)
	binary.BigEndian.PutUint16(be[:], 0)
	out = append(out, be[:]...)
	binary.BigEndian.PutUint16(be[:], number)
	out = append(out, be[:]...)
	out = append(out, payload...)
	return out
}

// Decode parses a response buffer.
func Decode(buf []byte, encRand []byte, encrypted bool) (*Frame, error) {
	if len(buf) < 10 {
		return nil, fmt.Errorf("frame too short: %d", len(buf))
	}
	if buf[0] != magic[0] || buf[1] != magic[1] {
		return nil, fmt.Errorf("bad magic %x", buf[:2])
	}
	tag := binary.BigEndian.Uint16(buf[2:4])
	seq := binary.BigEndian.Uint16(buf[4:6])
	crcTarget := binary.BigEndian.Uint16(buf[6:8])
	length := binary.BigEndian.Uint16(buf[8:10])

	// Heuristic: extended format if length>=24 and buf[10:12]==14
	extended := false
	if int(length) <= len(buf) && length >= 24 && binary.BigEndian.Uint16(buf[10:12]) == 14 {
		extended = true
	}

	var payload []byte
	if extended {
		if int(length) > len(buf) {
			return nil, fmt.Errorf("extended truncated: want %d have %d", length, len(buf))
		}
		if int(length) < 24 {
			return nil, fmt.Errorf("extended length too small: %d", length)
		}
		payload = buf[24:length]
		if CRC16(payload) != crcTarget {
			return nil, fmt.Errorf("extended CRC mismatch")
		}
		dtuSerial := binary.BigEndian.Uint64(buf[12:20])
		number := binary.BigEndian.Uint16(buf[22:24])
		return &Frame{Tag: tag, Seq: seq, Extended: true, DTUSerial: dtuSerial, Number: number, Payload: payload}, nil
	}

	useCrypto := encrypted && !notEncrypted(tag)
	var body []byte
	if useCrypto {
		// ciphertext lives from 10 .. len(buf); expected_length = read_length+16 in Python
		if len(buf) < 26 {
			return nil, fmt.Errorf("encrypted frame too short: %d", len(buf))
		}
		ct := buf[10:]
		plain, err := Crypt(false, encRand, tag, seq, ct)
		if err != nil {
			return nil, fmt.Errorf("decrypt: %w", err)
		}
		// CRC covers plaintext without tag in Python? Actually Python CRCs
		// buffer[10:read_length] which is ciphertext-minus-tag for encrypted.
		// Verify against ciphertext without GCM tag:
		crcBody := buf[10 : len(buf)-16]
		if CRC16(crcBody) != crcTarget {
			// some firmwares CRC the plaintext; accept either
			if CRC16(plain) != crcTarget {
				return nil, fmt.Errorf("encrypted CRC mismatch")
			}
		}
		payload = plain
	} else {
		if int(length) > len(buf) {
			return nil, fmt.Errorf("truncated: want %d have %d", length, len(buf))
		}
		body = buf[10:length]
		if CRC16(body) != crcTarget {
			return nil, fmt.Errorf("CRC mismatch")
		}
		payload = body
	}

	return &Frame{Tag: tag, Seq: seq, Payload: payload}, nil
}
