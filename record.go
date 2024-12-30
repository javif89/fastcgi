package fastcgi

import (
	"bytes"
	"encoding/binary"
)

// for padding so we don't have to allocate all the time
// not synchronized because we don't care what the contents are
var pad [maxPad]byte

// A record is essentially a "packet" in FastCGI.
// The header lets the server know what type
// of data is being sent, and it expects
// a certain structure depending on
// the type.
type Header struct {
	Version       uint8
	Type          FCGIRecordType
	Id            uint16
	ContentLength uint16
	PaddingLength uint8
	Reserved      uint8
}

type Record struct {
	Header     Header
	Content    []byte
	ReadBuffer []byte // Buffer to use when reading a response
}

// Turn a record into a byte array so it can be
// sent over the network. The byte array will
// be in the shape/order that is expected
// in the FastCGI protocol.
func (r *Record) toBytes() ([]byte, error) {
	var buf bytes.Buffer

	if err := binary.Write(&buf, binary.BigEndian, r.Header); err != nil {
		return nil, err
	}
	if _, err := buf.Write(r.Content); err != nil {
		return nil, err
	}
	if _, err := buf.Write(pad[:r.Header.PaddingLength]); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (req *FCGIRequest) NewRecord(t FCGIRecordType, content []byte) *Record {
	r := Record{}

	r.Header.Version = 1
	r.Header.Type = t
	r.Header.Id = req.Id
	r.Header.ContentLength = uint16(len(content))
	r.Header.PaddingLength = 0
	r.Content = content

	return &r
}

// FCGI_BEGIN_REQUEST record should
// have a body of 8 bytes with:
// - The first byte being the role
// - The second byte being also the role
// - The third byte being the flags
// - The last five bytes are reserved for future use
func (req *FCGIRequest) NewBeginRequestRecord() *Record {
	role := uint16(FCGI_RESPONDER)
	flags := byte(0)
	// Create an 8-byte array as per the FastCGI specification.
	var b [8]byte

	// Split the 16-bit role into two bytes and assign them.
	b[0] = byte(role >> 8) // High byte
	b[1] = byte(role)      // Low byte

	// Set the flags.
	b[2] = flags

	// The reserved bytes (b[3] to b[7]) will remain zero by default.

	// Return a begin request record
	return req.NewRecord(FCGI_BEGIN_REQUEST, b[:])
}
