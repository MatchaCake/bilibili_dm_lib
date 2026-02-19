package dm

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"

	"github.com/andybalholm/brotli"
)

// Packet protocol versions.
const (
	ProtoCommand       uint16 = 0 // Raw JSON command
	ProtoSpecial       uint16 = 1 // Special (heartbeat, auth)
	ProtoCommandZlib   uint16 = 2 // Zlib-compressed commands
	ProtoCommandBrotli uint16 = 3 // Brotli-compressed commands
)

// Packet operation types.
const (
	OpHeartbeat       uint32 = 2
	OpHeartbeatReply  uint32 = 3
	OpCommand         uint32 = 5
	OpCertificate     uint32 = 7
	OpCertificateResp uint32 = 8
)

const headerSize = 16

// Packet represents a single Bilibili danmaku protocol packet.
type Packet struct {
	Protocol uint16
	OpType   uint32
	Sequence uint32
	Body     []byte
}

// encodePacket serializes a Packet into the binary wire format.
func encodePacket(p *Packet) []byte {
	totalSize := uint32(headerSize) + uint32(len(p.Body))
	buf := make([]byte, totalSize)

	binary.BigEndian.PutUint32(buf[0:4], totalSize)
	binary.BigEndian.PutUint16(buf[4:6], headerSize)
	binary.BigEndian.PutUint16(buf[6:8], p.Protocol)
	binary.BigEndian.PutUint32(buf[8:12], p.OpType)
	binary.BigEndian.PutUint32(buf[12:16], p.Sequence)
	copy(buf[headerSize:], p.Body)

	return buf
}

// buildAuthPacket creates the authentication packet sent after WebSocket connect.
func buildAuthPacket(roomID int64, token string) []byte {
	protover := 3
	if token == "" {
		protover = 2 // fallback to zlib when no auth token
	}
	body := map[string]interface{}{
		"uid":      0,
		"roomid":   roomID,
		"key":      token,
		"protover": protover,
	}
	data, err := json.Marshal(body)
	if err != nil {
		// Should never happen with primitive values; panic to surface programming errors.
		panic(fmt.Sprintf("buildAuthPacket: marshal auth body: %v", err))
	}
	return encodePacket(&Packet{
		Protocol: ProtoSpecial,
		OpType:   OpCertificate,
		Sequence: 1,
		Body:     data,
	})
}

// buildHeartbeatPacket creates a heartbeat packet.
func buildHeartbeatPacket() []byte {
	return encodePacket(&Packet{
		Protocol: ProtoSpecial,
		OpType:   OpHeartbeat,
		Sequence: 1,
		Body:     []byte("[Object object]"),
	})
}

// decodePackets parses raw bytes into one or more Packets, handling
// compression (Brotli/Zlib) and nested packet structures.
func decodePackets(data []byte) ([]*Packet, error) {
	if len(data) < headerSize {
		return nil, fmt.Errorf("data too short: %d bytes", len(data))
	}

	var packets []*Packet
	for len(data) >= headerSize {
		totalSize := binary.BigEndian.Uint32(data[0:4])
		if int(totalSize) > len(data) || totalSize < headerSize {
			return nil, fmt.Errorf("invalid packet size: %d (remaining %d)", totalSize, len(data))
		}

		proto := binary.BigEndian.Uint16(data[6:8])
		opType := binary.BigEndian.Uint32(data[8:12])
		seq := binary.BigEndian.Uint32(data[12:16])
		body := data[headerSize:totalSize]

		switch proto {
		case ProtoCommandBrotli:
			decompressed, err := decompressBrotli(body)
			if err != nil {
				return nil, fmt.Errorf("brotli decompress: %w", err)
			}
			nested, err := decodePackets(decompressed)
			if err != nil {
				return nil, fmt.Errorf("decode nested brotli packets: %w", err)
			}
			packets = append(packets, nested...)

		case ProtoCommandZlib:
			decompressed, err := decompressZlib(body)
			if err != nil {
				return nil, fmt.Errorf("zlib decompress: %w", err)
			}
			nested, err := decodePackets(decompressed)
			if err != nil {
				return nil, fmt.Errorf("decode nested zlib packets: %w", err)
			}
			packets = append(packets, nested...)

		default:
			packets = append(packets, &Packet{
				Protocol: proto,
				OpType:   opType,
				Sequence: seq,
				Body:     body,
			})
		}

		data = data[totalSize:]
	}

	return packets, nil
}

func decompressBrotli(data []byte) ([]byte, error) {
	reader := brotli.NewReader(bytes.NewReader(data))
	return io.ReadAll(reader)
}

func decompressZlib(data []byte) ([]byte, error) {
	reader, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}
