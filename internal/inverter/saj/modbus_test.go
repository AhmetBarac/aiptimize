package saj

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// fakeInverter answers Modbus TCP reads from a register map, so the transport
// can be tested without hardware.
type fakeInverter struct {
	regs   map[uint16]uint16
	writes []uint16
}

func (f *fakeInverter) serve(t *testing.T, ln net.Listener) {
	t.Helper()
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			for {
				header := make([]byte, 7)
				if _, err := io.ReadFull(conn, header); err != nil {
					return
				}
				length := int(binary.BigEndian.Uint16(header[4:6]))
				body := make([]byte, length-1)
				if _, err := io.ReadFull(conn, body); err != nil {
					return
				}
				tx := header[0:2]
				function := body[0]
				switch function {
				case fcReadHolding:
					addr := binary.BigEndian.Uint16(body[1:3])
					count := int(binary.BigEndian.Uint16(body[3:5]))
					// Transaction id, protocol id, length, unit, function, byte count.
					resp := make([]byte, 0, 9+count*2)
					resp = append(resp, tx...)
					resp = append(resp, 0, 0, 0, byte(3+count*2), 1, function, byte(count*2))
					for i := 0; i < count; i++ {
						value := f.regs[addr+uint16(i)]
						resp = append(resp, byte(value>>8), byte(value))
					}
					if _, err := conn.Write(resp); err != nil {
						return
					}
				case fcWriteSingle, fcWriteMulti:
					addr := binary.BigEndian.Uint16(body[1:3])
					f.writes = append(f.writes, addr)
					resp := append([]byte{}, tx...)
					resp = append(resp, 0, 0, 0, 6, 1, body[0])
					resp = append(resp, body[1:5]...)
					if _, err := conn.Write(resp); err != nil {
						return
					}
				default:
					return
				}
			}
		}()
	}
}

func startFake(t *testing.T, regs map[uint16]uint16) (*transport, *fakeInverter) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	fake := &fakeInverter{regs: regs}
	go fake.serve(t, ln)
	tr := newTransport(ln.Addr().String(), 1, 2*time.Second)
	t.Cleanup(func() { _ = tr.close() })
	return tr, fake
}

func TestReadHoldingRegisters(t *testing.T) {
	tr, _ := startFake(t, map[uint16]uint16{0x100: 12, 0x101: 34, 0x102: 56})

	regs, err := tr.readHolding(context.Background(), 0x100, 3)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	want := []uint16{12, 34, 56}
	for i := range want {
		if regs[i] != want[i] {
			t.Fatalf("register %d = %d, want %d", i, regs[i], want[i])
		}
	}
}

func TestWriteSingleAndMultiple(t *testing.T) {
	tr, fake := startFake(t, map[uint16]uint16{})

	if err := tr.writeRegisters(context.Background(), 0x3247, []uint16{3}); err != nil {
		t.Fatalf("single write: %v", err)
	}
	if err := tr.writeRegisters(context.Background(), 0x3636, []uint16{2, 15}); err != nil {
		t.Fatalf("multi write: %v", err)
	}
	if len(fake.writes) != 2 {
		t.Fatalf("writes = %v", fake.writes)
	}
}

func TestRejectedRequestIsReported(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		header := make([]byte, 7)
		if _, err := io.ReadFull(conn, header); err != nil {
			return
		}
		body := make([]byte, int(binary.BigEndian.Uint16(header[4:6]))-1)
		if _, err := io.ReadFull(conn, body); err != nil {
			return
		}
		// Exception response: function|0x80, illegal data address.
		resp := []byte{header[0], header[1], 0, 0, 0, 3, 1, fcReadHolding | 0x80, 0x02}
		_, _ = conn.Write(resp)
	}()

	tr := newTransport(ln.Addr().String(), 1, time.Second)
	defer tr.close()

	_, err = tr.readHolding(context.Background(), 0x100, 1)
	var modbusErr *ModbusError
	if !errors.As(err, &modbusErr) {
		t.Fatalf("err = %v, want ModbusError", err)
	}
	if modbusErr.Code != 0x02 {
		t.Fatalf("code = %d, want 2", modbusErr.Code)
	}
}
