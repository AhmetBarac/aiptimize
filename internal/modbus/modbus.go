// Package modbus is a small Modbus TCP client shared by the inverter adapters.
//
// It speaks the standard application data unit: a transaction id, a protocol id
// of zero, a length, the unit identifier, then the function code and payload.
// One connection is kept open and requests are serialised, because inverters
// answer on a single socket and dislike overlapping traffic.
package modbus

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// Function codes used by the adapters.
const (
	FCReadHolding = 0x03
	FCWriteSingle = 0x06
	FCWriteMulti  = 0x10
)

// ErrUnreachable is returned when the device cannot be reached.
var ErrUnreachable = errors.New("modbus: device is unreachable")

// ProtocolError is a rejection from the device.
type ProtocolError struct {
	Function byte
	Code     byte
}

func (e *ProtocolError) Error() string {
	return fmt.Sprintf("modbus: function 0x%02x rejected with code 0x%02x", e.Function, e.Code)
}

// Config describes a Modbus TCP endpoint.
type Config struct {
	// Address is host:port.
	Address string
	// UnitID is the Modbus station address.
	UnitID byte
	// Timeout bounds one request.
	Timeout time.Duration
	// Pause is inserted after a successful request. Some devices answer the
	// next request with the previous frame when asked too quickly.
	Pause time.Duration
}

// Client is a Modbus TCP connection.
type Client struct {
	cfg    Config
	dialer net.Dialer

	mu   sync.Mutex
	conn net.Conn
	txID uint16
}

// New builds a client. Nothing is dialled until the first request.
func New(cfg Config) *Client {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.Pause <= 0 {
		cfg.Pause = 20 * time.Millisecond
	}
	return &Client{cfg: cfg, dialer: net.Dialer{Timeout: cfg.Timeout}}
}

// Close releases the connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return err
}

func (c *Client) ensureLocked(ctx context.Context) (net.Conn, error) {
	if c.conn != nil {
		return c.conn, nil
	}
	conn, err := c.dialer.DialContext(ctx, "tcp", c.cfg.Address)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	c.conn = conn
	return conn, nil
}

func (c *Client) dropLocked() {
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
}

// roundTrip sends one request and returns the payload after the function code.
func (c *Client) roundTrip(ctx context.Context, function byte, payload []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	conn, err := c.ensureLocked(ctx)
	if err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(c.cfg.Timeout))
	}

	c.txID++
	req := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint16(req[0:], c.txID)
	binary.BigEndian.PutUint16(req[2:], 0) // protocol id
	binary.BigEndian.PutUint16(req[4:], uint16(len(payload)+2))
	req[6] = c.cfg.UnitID
	req[7] = function
	copy(req[8:], payload)

	if _, err := conn.Write(req); err != nil {
		c.dropLocked()
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}

	header := make([]byte, 7)
	if _, err := io.ReadFull(conn, header); err != nil {
		c.dropLocked()
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	length := int(binary.BigEndian.Uint16(header[4:6]))
	if length < 2 || length > 260 {
		c.dropLocked()
		return nil, fmt.Errorf("modbus: implausible length %d", length)
	}
	body := make([]byte, length-1) // the unit id was part of the length
	if _, err := io.ReadFull(conn, body); err != nil {
		c.dropLocked()
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}

	got := body[0]
	if got == function|0x80 {
		if len(body) < 2 {
			return nil, &ProtocolError{Function: function}
		}
		return nil, &ProtocolError{Function: function, Code: body[1]}
	}
	if got != function {
		c.dropLocked()
		return nil, fmt.Errorf("modbus: unexpected function 0x%02x for request 0x%02x", got, function)
	}
	time.Sleep(c.cfg.Pause)
	return body[1:], nil
}

// ReadHolding reads count holding registers starting at addr.
func (c *Client) ReadHolding(ctx context.Context, addr, count uint16) ([]uint16, error) {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint16(payload[0:], addr)
	binary.BigEndian.PutUint16(payload[2:], count)

	out, err := c.roundTrip(ctx, FCReadHolding, payload)
	if err != nil {
		return nil, err
	}
	if len(out) < 1 {
		return nil, errors.New("modbus: empty read response")
	}
	n := int(out[0])
	if n != int(count)*2 || len(out) < 1+n {
		return nil, fmt.Errorf("modbus: read %d bytes, expected %d", len(out)-1, count*2)
	}
	regs := make([]uint16, count)
	for i := 0; i < int(count); i++ {
		regs[i] = binary.BigEndian.Uint16(out[1+i*2:])
	}
	return regs, nil
}

// WriteRegisters writes values starting at addr, choosing single or multiple
// write by length.
func (c *Client) WriteRegisters(ctx context.Context, addr uint16, values []uint16) error {
	if len(values) == 0 {
		return errors.New("modbus: nothing to write")
	}
	if len(values) == 1 {
		payload := make([]byte, 4)
		binary.BigEndian.PutUint16(payload[0:], addr)
		binary.BigEndian.PutUint16(payload[2:], values[0])
		_, err := c.roundTrip(ctx, FCWriteSingle, payload)
		return err
	}
	payload := make([]byte, 5+2*len(values))
	binary.BigEndian.PutUint16(payload[0:], addr)
	binary.BigEndian.PutUint16(payload[2:], uint16(len(values)))
	payload[4] = byte(len(values) * 2)
	for i, value := range values {
		binary.BigEndian.PutUint16(payload[5+i*2:], value)
	}
	_, err := c.roundTrip(ctx, FCWriteMulti, payload)
	return err
}
