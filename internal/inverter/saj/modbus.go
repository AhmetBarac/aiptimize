// Package saj reads and controls a SAJ H1 hybrid inverter over Modbus TCP.
//
// The frame is the standard Modbus TCP application data unit: a two-byte
// transaction id, a protocol id of zero, a length, the unit identifier and then
// the function code with its payload. The SAJ answers on unit 1 and needs a
// small pause between requests, so a connection is kept open and reused.
package saj

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

// Modbus function codes used by the inverter.
const (
	fcReadHolding = 0x03
	fcWriteSingle = 0x06
	fcWriteMulti  = 0x10
)

// ErrNoConnection is returned when the inverter cannot be reached.
var ErrNoConnection = errors.New("saj: inverter is unreachable")

// ModbusError is a protocol-level rejection from the device.
type ModbusError struct {
	Function byte
	Code     byte
}

func (e *ModbusError) Error() string {
	return fmt.Sprintf("saj: modbus function 0x%02x rejected with code 0x%02x", e.Function, e.Code)
}

// transport is a minimal Modbus TCP client. It is safe for concurrent use: the
// inverter is a single device, so requests are serialised.
type transport struct {
	addr    string
	unitID  byte
	timeout time.Duration
	dialer  net.Dialer

	mu    sync.Mutex
	conn  net.Conn
	txID  uint16
	pause time.Duration
}

func newTransport(addr string, unitID byte, timeout time.Duration) *transport {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &transport{
		addr:    addr,
		unitID:  unitID,
		timeout: timeout,
		dialer:  net.Dialer{Timeout: timeout},
		// The H1 needs a moment between requests; without it the next read can
		// be answered with the previous frame.
		pause: 20 * time.Millisecond,
	}
}

func (t *transport) close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.conn == nil {
		return nil
	}
	err := t.conn.Close()
	t.conn = nil
	return err
}

// ensureLocked dials if needed. Callers hold the mutex.
func (t *transport) ensureLocked(ctx context.Context) (net.Conn, error) {
	if t.conn != nil {
		return t.conn, nil
	}
	conn, err := t.dialer.DialContext(ctx, "tcp", t.addr)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoConnection, err)
	}
	t.conn = conn
	return conn, nil
}

// dropLocked closes a connection that has gone bad. Callers hold the mutex.
func (t *transport) dropLocked() {
	if t.conn != nil {
		_ = t.conn.Close()
		t.conn = nil
	}
}

// roundTrip sends one request and returns the payload after the function code.
func (t *transport) roundTrip(ctx context.Context, function byte, payload []byte) ([]byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	conn, err := t.ensureLocked(ctx)
	if err != nil {
		return nil, err
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(t.timeout))
	}

	t.txID++
	req := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint16(req[0:], t.txID)
	binary.BigEndian.PutUint16(req[2:], 0) // protocol id
	binary.BigEndian.PutUint16(req[4:], uint16(len(payload)+2))
	req[6] = t.unitID
	req[7] = function
	copy(req[8:], payload)

	if _, err := conn.Write(req); err != nil {
		t.dropLocked()
		return nil, fmt.Errorf("%w: %v", ErrNoConnection, err)
	}

	header := make([]byte, 7)
	if _, err := io.ReadFull(conn, header); err != nil {
		t.dropLocked()
		return nil, fmt.Errorf("%w: %v", ErrNoConnection, err)
	}
	length := int(binary.BigEndian.Uint16(header[4:6]))
	if length < 2 || length > 260 {
		t.dropLocked()
		return nil, fmt.Errorf("saj: implausible Modbus length %d", length)
	}
	body := make([]byte, length-1) // the unit id was part of the length
	if _, err := io.ReadFull(conn, body); err != nil {
		t.dropLocked()
		return nil, fmt.Errorf("%w: %v", ErrNoConnection, err)
	}

	got := body[0]
	if got == function|0x80 {
		if len(body) < 2 {
			return nil, &ModbusError{Function: function, Code: 0}
		}
		return nil, &ModbusError{Function: function, Code: body[1]}
	}
	if got != function {
		t.dropLocked()
		return nil, fmt.Errorf("saj: unexpected function 0x%02x for request 0x%02x", got, function)
	}
	time.Sleep(t.pause)
	return body[1:], nil
}

// readHolding reads count holding registers starting at addr.
func (t *transport) readHolding(ctx context.Context, addr, count uint16) ([]uint16, error) {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint16(payload[0:], addr)
	binary.BigEndian.PutUint16(payload[2:], count)

	out, err := t.roundTrip(ctx, fcReadHolding, payload)
	if err != nil {
		return nil, err
	}
	if len(out) < 1 {
		return nil, errors.New("saj: empty read response")
	}
	n := int(out[0])
	if n != int(count)*2 || len(out) < 1+n {
		return nil, fmt.Errorf("saj: read %d bytes, expected %d", len(out)-1, count*2)
	}
	regs := make([]uint16, count)
	for i := 0; i < int(count); i++ {
		regs[i] = binary.BigEndian.Uint16(out[1+i*2:])
	}
	return regs, nil
}

// writeRegisters writes values starting at addr using the correct function code.
func (t *transport) writeRegisters(ctx context.Context, addr uint16, values []uint16) error {
	if len(values) == 0 {
		return errors.New("saj: nothing to write")
	}
	if len(values) == 1 {
		payload := make([]byte, 4)
		binary.BigEndian.PutUint16(payload[0:], addr)
		binary.BigEndian.PutUint16(payload[2:], values[0])
		_, err := t.roundTrip(ctx, fcWriteSingle, payload)
		return err
	}
	payload := make([]byte, 5+2*len(values))
	binary.BigEndian.PutUint16(payload[0:], addr)
	binary.BigEndian.PutUint16(payload[2:], uint16(len(values)))
	payload[4] = byte(len(values) * 2)
	for i, value := range values {
		binary.BigEndian.PutUint16(payload[5+i*2:], value)
	}
	_, err := t.roundTrip(ctx, fcWriteMulti, payload)
	return err
}
