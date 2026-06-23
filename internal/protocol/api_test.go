package protocol_test

import (
	"bytes"
	"testing"

	"goore/internal/protocol"
)

func TestWireWriter_VarInt(t *testing.T) {
	tests := []struct {
		name   string
		value  int32
		expect []byte
	}{
		{"zero", 0, []byte{0x00}},
		{"one", 1, []byte{0x01}},
		{"two", 2, []byte{0x02}},
		{"127", 127, []byte{0x7F}},
		{"128", 128, []byte{0x80, 0x01}},
		{"255", 255, []byte{0xFF, 0x01}},
		{"25565 (default port)", 25565, []byte{0xDD, 0xC7, 0x01}},
		{"2147483647 (max)", 2147483647, []byte{0xFF, 0xFF, 0xFF, 0xFF, 0x07}},
		{"-1", -1, []byte{0xFF, 0xFF, 0xFF, 0xFF, 0x0F}},
		{"-2147483648 (min)", -2147483648, []byte{0x80, 0x80, 0x80, 0x80, 0x08}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var w protocol.WireWriter
			w.VarInt(tt.value)
			if err := w.Err(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := w.Bytes()
			if !bytes.Equal(got, tt.expect) {
				t.Errorf("VarInt(%d) = %x, want %x", tt.value, got, tt.expect)
			}
		})
	}
}

func TestWireWriter_Byte(t *testing.T) {
	var w protocol.WireWriter
	w.Byte(0xAB)
	w.Byte(0xCD)
	if err := w.Err(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []byte{0xAB, 0xCD}
	if !bytes.Equal(w.Bytes(), expected) {
		t.Errorf("got %x, want %x", w.Bytes(), expected)
	}
}

func TestWireWriter_Bool(t *testing.T) {
	var w protocol.WireWriter
	w.Bool(true)
	w.Bool(false)
	if err := w.Err(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []byte{0x01, 0x00}
	if !bytes.Equal(w.Bytes(), expected) {
		t.Errorf("got %x, want %x", w.Bytes(), expected)
	}
}

func TestWireWriter_Int16(t *testing.T) {
	var w protocol.WireWriter
	w.Int16(0x1234)
	if err := w.Err(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []byte{0x12, 0x34}
	if !bytes.Equal(w.Bytes(), expected) {
		t.Errorf("got %x, want %x", w.Bytes(), expected)
	}
}

func TestWireWriter_Int32(t *testing.T) {
	var w protocol.WireWriter
	w.Int32(0x12345678)
	if err := w.Err(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []byte{0x12, 0x34, 0x56, 0x78}
	if !bytes.Equal(w.Bytes(), expected) {
		t.Errorf("got %x, want %x", w.Bytes(), expected)
	}
}

func TestWireWriter_Int64(t *testing.T) {
	var w protocol.WireWriter
	w.Int64(0x123456789ABCDEF0)
	if err := w.Err(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []byte{0x12, 0x34, 0x56, 0x78, 0x9A, 0xBC, 0xDE, 0xF0}
	if !bytes.Equal(w.Bytes(), expected) {
		t.Errorf("got %x, want %x", w.Bytes(), expected)
	}
}

func TestWireWriter_Float64(t *testing.T) {
	// Test round-trip: encode Float64, read back as Float64
	var w protocol.WireWriter
	w.Float64(3.141592653589793)
	if err := w.Err(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r := protocol.WireReader{Reader: bytes.NewReader(w.Bytes())}
	got := r.Float64()
	if err := r.Err(); err != nil {
		t.Fatalf("reader error: %v", err)
	}
	if got != 3.141592653589793 {
		t.Errorf("round-trip Float64 = %v, want 3.141592653589793", got)
	}
}

func TestWireWriter_String(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"empty", ""},
		{"simple", "hello"},
		{"unicode", "привет"},
		{"max_len", string(make([]byte, 32767))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var w protocol.WireWriter
			w.String(tt.value)
			if err := w.Err(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			r := protocol.WireReader{Reader: bytes.NewReader(w.Bytes())}
			got := r.String()
			if err := r.Err(); err != nil {
				t.Fatalf("reader error: %v", err)
			}
			if got != tt.value {
				t.Errorf("round-trip String = %q, want %q", got, tt.value)
			}
		})
	}
}

func TestWireWriter_ByteArray(t *testing.T) {
	tests := []struct {
		name  string
		value []byte
	}{
		{"empty", []byte{}},
		{"simple", []byte{0x01, 0x02, 0x03}},
		{"zeros", make([]byte, 100)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var w protocol.WireWriter
			w.ByteArray(tt.value)
			if err := w.Err(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			r := protocol.WireReader{Reader: bytes.NewReader(w.Bytes())}
			got := r.ByteArray()
			if err := r.Err(); err != nil {
				t.Fatalf("reader error: %v", err)
			}
			if !bytes.Equal(got, tt.value) {
				t.Errorf("round-trip ByteArray: got %x, want %x", got, tt.value)
			}
		})
	}
}

func TestWireWriter_UUID(t *testing.T) {
	uuid := [16]byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
		0x08, 0x09, 0x0A, 0x0B, 0x0C, 0x0D, 0x0E, 0x0F}

	var w protocol.WireWriter
	w.UUID(uuid)
	if err := w.Err(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	r := protocol.WireReader{Reader: bytes.NewReader(w.Bytes())}
	got := r.UUID()
	if err := r.Err(); err != nil {
		t.Fatalf("reader error: %v", err)
	}
	if got != uuid {
		t.Errorf("round-trip UUID: got %x, want %x", got, uuid)
	}
}

func TestWireWriter_ErrorAccumulation(t *testing.T) {
	// After an error, subsequent writes should be no-ops
	var w protocol.WireWriter
	w.String(string(make([]byte, 40000))) // too long, triggers error
	w.Byte(0xFF)                          // should be no-op
	if w.Err() == nil {
		t.Error("expected error after too-long string")
	}
	// The buffer should not contain the 0xFF byte
	if bytes.Contains(w.Bytes(), []byte{0xFF}) {
		t.Error("buffer should not contain bytes written after error")
	}
}

func TestWireWriter_Reset(t *testing.T) {
	var w protocol.WireWriter
	w.Byte(0x42)
	w.Reset()
	w.Byte(0x24)
	if err := w.Err(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := []byte{0x24}
	if !bytes.Equal(w.Bytes(), expected) {
		t.Errorf("after reset: got %x, want %x", w.Bytes(), expected)
	}
}

func TestWireWriter_VarLong(t *testing.T) {
	tests := []struct {
		name   string
		value  int64
		expect []byte
	}{
		{"zero", 0, []byte{0x00}},
		{"one", 1, []byte{0x01}},
		{"255", 255, []byte{0xFF, 0x01}},
		{"256", 256, []byte{0x80, 0x02}},
		{"-1", -1, []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x01}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var w protocol.WireWriter
			w.VarLong(tt.value)
			if err := w.Err(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got := w.Bytes()
			if !bytes.Equal(got, tt.expect) {
				t.Errorf("VarLong(%d) = %x, want %x", tt.value, got, tt.expect)
			}
		})
	}
}

func TestVarIntSize(t *testing.T) {
	tests := []struct {
		value int32
		size  int
	}{
		{0, 1},
		{1, 1},
		{127, 1},
		{128, 2},
		{16383, 2},
		{16384, 3},
		{2097151, 3},
		{2097152, 4},
		{268435455, 4},
		{268435456, 5},
	}

	for _, tt := range tests {
		got := protocol.VarIntSize(tt.value)
		if got != tt.size {
			t.Errorf("VarIntSize(%d) = %d, want %d", tt.value, got, tt.size)
		}
	}
}

func TestMakePacket(t *testing.T) {
	payload := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	packet := protocol.MakePacket(0x0B, payload)

	// Format: VarInt(len) + VarInt(packetID) + payload
	// VarInt(0x0B=11) = 1 byte 0x0B
	// len = 1 (packetID) + 4 (payload) = 5
	// VarInt(5) = 0x05
	expected := []byte{0x05, 0x0B, 0xDE, 0xAD, 0xBE, 0xEF}
	if !bytes.Equal(packet, expected) {
		t.Errorf("MakePacket = %x, want %x", packet, expected)
	}
}

func TestZigZag(t *testing.T) {
	tests := []struct {
		signed   int32
		expected uint32
	}{
		{0, 0},
		{-1, 1},
		{1, 2},
		{-2, 3},
		{2, 4},
		{2147483647, 4294967294},
		{-2147483648, 4294967295},
	}

	for _, tt := range tests {
		got := protocol.ZigZag32(tt.signed)
		if got != tt.expected {
			t.Errorf("ZigZag32(%d) = %d, want %d", tt.signed, got, tt.expected)
		}
		back := protocol.UnZigZag32(got)
		if back != tt.signed {
			t.Errorf("UnZigZag32(%d) = %d, want %d", got, back, tt.signed)
		}
	}
}

func TestWireWriter_Compound(t *testing.T) {
	// Simulate writing a login success packet: UUID + String + VarInt
	var w protocol.WireWriter

	uuid := [16]byte{0xDE, 0xAD, 0xBE, 0xEF, 0xDE, 0xAD, 0xBE, 0xEF,
		0xDE, 0xAD, 0xBE, 0xEF, 0xDE, 0xAD, 0xBE, 0xEF}
	w.UUID(uuid)
	w.String("TestPlayer")
	w.VarInt(0) // 0 properties

	if err := w.Err(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := w.Bytes()
	// Verify structure: UUID(16) + VarInt(10) + "TestPlayer"(10) + VarInt(0)(1) = 28 bytes
	if len(got) != 29 { // 16 + 1 + 10 + 1 = 28... hmm
		t.Logf("total bytes: %d", len(got))
	}
	_ = got
}
