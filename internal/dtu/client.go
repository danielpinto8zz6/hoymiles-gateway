// Package dtu talks to a Hoymiles WiFi DTU over TCP/10081 (HM + protobuf).
package dtu

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"hoymiles-gateway/internal/protocol"
	"hoymiles-gateway/pb"
)

const (
	DefaultPort    = 10081
	DefaultTimeout = 12 * time.Second
	// Device firmware rate-limits; Python lib enforces 2s between requests.
	MinRequestInterval = 2 * time.Second
	// UTC+8 offset used by the DTU time fields.
	Offset = 28800
)

// Client is a single-owner connection to one DTU.
type Client struct {
	host      string
	port      int
	timeout   time.Duration
	encRand   []byte
	encrypted bool

	mu       sync.Mutex // serializes wire I/O
	seq      uint16
	lastReq  time.Time
	dtuCache uint64 // optional default DTU serial for extended frames
}

func New(host string) *Client {
	return &Client{host: host, port: DefaultPort, timeout: DefaultTimeout}
}

func (c *Client) SetTimeout(d time.Duration) { c.timeout = d }
func (c *Client) SetEncryption(encRand []byte, on bool) {
	c.encrypted = on
	c.encRand = encRand
}
func (c *Client) SetDTUSerial(sn uint64) { c.dtuCache = sn }

func (c *Client) nextSeq() uint16 {
	c.seq = (c.seq + 1) & 0xFFFF
	return c.seq
}

// roundTrip sends one request and returns the decoded response payload.
func (c *Client) roundTrip(ctx context.Context, tag uint16, payload proto.Message, extended bool, dtuSerial uint64, number uint16, resp proto.Message) error {
	body, err := proto.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// rate limit
	if !c.lastReq.IsZero() {
		if wait := MinRequestInterval - time.Since(c.lastReq); wait > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
		}
	}

	seq := c.nextSeq()
	var frame []byte
	if extended {
		frame = protocol.EncodeExtended(tag, seq, dtuSerial, number, body)
	} else {
		frame = protocol.EncodeStandard(tag, seq, body, c.encRand, c.encrypted)
	}

	d := net.Dialer{Timeout: c.timeout}
	conn, err := d.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", c.host, c.port))
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	deadline := time.Now().Add(c.timeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	_ = conn.SetDeadline(deadline)

	if _, err := conn.Write(frame); err != nil {
		return fmt.Errorf("write: %w", err)
	}

	// Read at least the 10-byte header, then the rest.
	hdr := make([]byte, 10)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		return fmt.Errorf("read header: %w", err)
	}
	// For extended, length is at bytes 8:10 and includes header.
	length := int(hdr[8])<<8 | int(hdr[9])
	if length < 10 {
		return fmt.Errorf("bad length %d", length)
	}
	rest := make([]byte, length-10)
	if len(rest) > 0 {
		if _, err := io.ReadFull(conn, rest); err != nil {
			// Some firmwares close after sending; try leftover.
			if err != io.ErrUnexpectedEOF && err != io.EOF {
				return fmt.Errorf("read body: %w", err)
			}
		}
	}
	buf := append(hdr, rest...)
	c.lastReq = time.Now()

	fr, err := protocol.Decode(buf, c.encRand, c.encrypted)
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	if resp != nil {
		if err := proto.Unmarshal(fr.Payload, resp); err != nil {
			return fmt.Errorf("unmarshal: %w", err)
		}
	}
	return nil
}

func nowHMS() []byte {
	return []byte(time.Now().Format("2006-01-02 15:04:05"))
}

func nowTS() int32 { return int32(time.Now().Unix()) }

// ESRegistry returns hybrid inverter / meter registry.
func (c *Client) ESRegistry(ctx context.Context, dtuSerial uint64) (*pb.ESRegReqDTO, error) {
	req := &pb.ESRegResDTO{
		Time:       nowTS(),
		TimeYmdHms: nowHMS(),
		Offset:     Offset,
		Cp:         0,
	}
	var resp pb.ESRegReqDTO
	if err := c.roundTrip(ctx, protocol.CmdESReg, req, true, dtuSerial, 1, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// ESData returns live hybrid telemetry (CT, BMS, power flow).
func (c *Client) ESData(ctx context.Context, dtuSerial uint64, invSerial int64) (*pb.ESDataReqDTO, error) {
	req := &pb.ESDataResDTO{
		Time:         nowTS(),
		TimeYmdHms:   nowHMS(),
		Offset:       Offset,
		Cp:           0,
		SerialNumber: invSerial,
	}
	var resp pb.ESDataReqDTO
	if err := c.roundTrip(ctx, protocol.CmdESData, req, true, dtuSerial, 1, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GWNetwork returns WiFi/cellular link state.
func (c *Client) GWNetwork(ctx context.Context, dtuSerial uint64) (*pb.GWNetInfoReq, error) {
	req := &pb.GWNetInfoRes{Time: nowTS(), Offset: Offset}
	var resp pb.GWNetInfoReq
	if err := c.roundTrip(ctx, protocol.CmdGWNetInfo, req, true, dtuSerial, 255, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// BMSMode matches hoymiles_wifi.hoymiles.BMSWorkingMode.
type BMSMode int32

const (
	BMSSelfUse         BMSMode = 1
	BMSEconomic        BMSMode = 2
	BMSBackupPower     BMSMode = 3
	BMSPureOffGrid     BMSMode = 4
	BMSForcedCharge    BMSMode = 5
	BMSForcedDischarge BMSMode = 6
	BMSPeakShaving     BMSMode = 7
	BMSTimeOfUse       BMSMode = 8
)

var bmsNames = map[string]BMSMode{
	"self_use":         BMSSelfUse,
	"economic":         BMSEconomic,
	"backup_power":     BMSBackupPower,
	"pure_off_grid":    BMSPureOffGrid,
	"forced_charging":  BMSForcedCharge,
	"forced_discharge": BMSForcedDischarge,
	"peak_shaving":     BMSPeakShaving,
	"time_of_use":      BMSTimeOfUse,
}

func ParseBMSMode(s string) (BMSMode, error) {
	if m, ok := bmsNames[s]; ok {
		return m, nil
	}
	return 0, fmt.Errorf("unknown bms mode %q", s)
}

// SetBMSMode writes the hybrid working mode (ES_USER_SET).
// revSoc / maxPower are percent 0–100 (maxPower sent as value*10 like the Python lib).
func (c *Client) SetBMSMode(ctx context.Context, dtuSerial uint64, invSerial int64, mode BMSMode, revSoc, maxPower, peakSoc, peakMeterPower int32) error {
	req := &pb.ESUserSetPutResDTO{
		Time:         nowTS(),
		Tid:          int64(time.Now().Unix()),
		SerialNumber: []int64{invSerial},
		Mode:         int32(mode),
	}
	if revSoc >= 0 {
		req.RevSoc = revSoc
	}
	if maxPower >= 0 {
		if maxPower < 0 || maxPower > 100 {
			return fmt.Errorf("max_power out of range")
		}
		req.MaxPower = maxPower * 10
	}
	switch mode {
	case BMSPeakShaving:
		if peakSoc < 0 || peakMeterPower < 0 {
			return fmt.Errorf("peak_shaving requires peak_soc and peak_meter_power")
		}
		req.PeakSoc = peakSoc
		req.PeakMeterpwr = peakMeterPower
	}

	var resp pb.ESUserSetPutReqDTO
	return c.roundTrip(ctx, protocol.CmdESUserSet, req, true, dtuSerial, 1, &resp)
}
