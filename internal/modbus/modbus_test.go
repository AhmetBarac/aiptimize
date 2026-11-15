package modbus

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// fakeDevice answers reads from a register map so framing can be checked
// without hardware.
type fakeDevice struct {
	regs   map[uint16]uint16
	writes []uint16
}

func (f *fakeDevice) serve(ln net.Listener) {
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
				switch body[0] {
				case FCReadHolding:
					addr := binary.BigEndian.Uint16(body[1:3])
					count := int(binary.BigEndian.Uint16(body[3:5]))
					resp := make([]byte, 0, 9+count*2)
					resp = append(resp, tx...)
					resp = append(resp, 0, 0, 0, byte(3+count*2), 1, FCReadHolding, byte(count*2))
					for i := 0; i < count; i++ {
						value := f.regs[addr+uint16(i)]
						resp = append(resp, byte(value>>8), byte(value))
					}
					if _, err := conn.Write(resp); err != nil {
						return
					}
				case FCWriteSingle, FCWriteMulti:
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

func startFake(t *testing.T, regs map[uint16]uint16) (*Client, *fakeDevice) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	fake := &fakeDevice{regs: regs}
	go fake.serve(ln)
	client := New(Config{Address: ln.Addr().String(), UnitID: 1, Timeout: 2 * time.Second})
	t.Cleanup(func() { _ = client.Close() })
	return client, fake
}

func TestReadHolding(t *testing.T) {
	client, _ := startFake(t, map[uint16]uint16{0x100: 12, 0x101: 34, 0x102: 56})

	regs, err := client.ReadHolding(context.Background(), 0x100, 3)
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

func TestWritesUseSingleAndMultiple(t *testing.T) {
	client, fake := startFake(t, map[uint16]uint16{})

	if err := client.WriteRegisters(context.Background(), 0x3247, []uint16{3}); err != nil {
		t.Fatalf("single write: %v", err)
	}
	if err := client.WriteRegisters(context.Background(), 0x3636, []uint16{2, 15}); err != nil {
		t.Fatalf("multi write: %v", err)
	}
	if len(fake.writes) != 2 {
		t.Fatalf("writes = %v", fake.writes)
	}
}

func TestRejectionIsTyped(t *testing.T) {
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
		// Illegal data address.
		_, _ = conn.Write([]byte{header[0], header[1], 0, 0, 0, 3, 1, FCReadHolding | 0x80, 0x02})
	}()

	client := New(Config{Address: ln.Addr().String(), UnitID: 1, Timeout: time.Second})
	defer client.Close()

	_, err = client.ReadHolding(context.Background(), 0x100, 1)
	var protocolErr *ProtocolError
	if !errors.As(err, &protocolErr) || protocolErr.Code != 0x02 {
		t.Fatalf("err = %v, want a ProtocolError with code 2", err)
	}
}

func TestUnreachableIsTyped(t *testing.T) {
	client := New(Config{Address: "127.0.0.1:1", UnitID: 1, Timeout: 200 * time.Millisecond})
	defer client.Close()

	_, err := client.ReadHolding(context.Background(), 0x100, 1)
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("err = %v, want ErrUnreachable", err)
	}
}
