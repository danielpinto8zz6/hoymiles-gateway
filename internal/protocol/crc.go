package protocol

// CRC-16/MODBUS (poly 0x18005 reflected / 0x8005, init 0xFFFF).
// Matches crcmod mkCrcFun(0x18005, rev=True, initCrc=0xFFFF, xorOut=0x0000).

var crc16table [256]uint16

func init() {
	for i := 0; i < 256; i++ {
		crc := uint16(i)
		for j := 0; j < 8; j++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0xA001
			} else {
				crc >>= 1
			}
		}
		crc16table[i] = crc
	}
}

func CRC16(data []byte) uint16 {
	crc := uint16(0xFFFF)
	for _, b := range data {
		crc = (crc >> 8) ^ crc16table[byte(crc)^b]
	}
	return crc
}
