package fastcgi

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
)

type FCGIRequest struct {
	Id      uint16
	Context map[string]string
	Body    bytes.Buffer
	Records []Record
}

func RequestFromHttp(r *http.Request) *FCGIRequest {
	c := FCGIRequest{}
	c.Id = 1
	c.Context = make(map[string]string)
	c.Context["SERVER_SOFTWARE"] = "oasis / fastcgi"
	c.Context["QUERY_STRING"] = r.URL.RawQuery
	c.Context["REMOTE_ADDR"] = "127.0.0.1"
	c.Context["REQUEST_METHOD"] = r.Method
	c.Context["REQUEST_URI"] = r.URL.Path
	c.Context["SERVER_ADDR"] = "localhost"
	c.Context["SERVER_PORT"] = "8000"
	c.Context["SERVER_NAME"] = "localhost"

	// HTTP headers should be sent as FCGI_PARAMS.
	// We have to turn the name of the header
	// into environment variable format.
	// Ex: Content-Type => CONTENT_TYPE
	// Parameters like CONTENT_TYPE or
	// CONTENT_LENGTH are important, and
	// they should come from the browser/request
	// itself. If you're having issues, check
	// if some important parameter is missing.
	for name, value := range r.Header {
		// FastCGI doesn't support multiple values per header.
		// However, the go http library does, so we'll
		// concatenate the values with , just in case.
		k := strings.ToUpper(name)
		k = strings.ReplaceAll(k, "-", "_")
		c.Context[k] = strings.Join(value, ", ")
		// TODO: In the future we can figure out which headers need the
		// HTTP_ prefix. But for now we'll just add both params with
		// and without the prefix.
		c.Context[fmt.Sprintf("HTTP_%s", k)] = strings.Join(value, ", ")
	}

	// HTTP body will be forwarded in FCGI_STDIN
	body, err := io.ReadAll(r.Body)

	if err != nil {
		panic("Somehow failed at reading the http body")
	}

	c.Body.Write(body)

	return &c
}

func (req *FCGIRequest) Script(filename string) {
	req.Context["SCRIPT_FILENAME"] = filepath.Join(req.Context["DOCUMENT_ROOT"], filename)
}

func (req *FCGIRequest) Root(path string) {
	req.Context["DOCUMENT_ROOT"] = path
}

// The body of the http response (such as POST form data)
// will be encoded into records of type FCGI_STDIN
// to be sent as a stream. If the body is longer
// than maxWrite (in bytes) we will split it into separate
// records. The value of maxWrite is determined by
// the size of the ContentLength field of the
// Header struct. Since it's only a two byte
// integer, the max content length we can
// encode in a single record is 65,535 bytes.
func (req *FCGIRequest) EncodeBody() []Record {
	// We made the request body a bytes.Buffer so the
	// operation of splitting it into multiple
	// records can be done by just reading
	// from the buffer up to maxWrite
	// until it's done.
	chunks := [][]byte{}

	for len(req.Body.Bytes()) > 0 {
		// Read either max write or the current buffer length,
		// whichever is higher.
		readSize := min(len(req.Body.Bytes()), maxWrite)
		chunk := make([]byte, readSize)
		req.Body.Read(chunk)
		chunks = append(chunks, chunk)
	}

	// Pack up the chunks into records
	records := []Record{}

	for _, c := range chunks {
		records = append(records, *req.NewRecord(FCGI_STDIN, c))
	}

	return records
}

// Spec: https://www.mit.edu/~yandros/doc/specs/fcgi-spec.html#S3
// Name value pairs such as: SCRIPT_PATH = /some/path
// Should be encoded as such:
// Name size
// Value size
// Name
// Value
// We'll encode the context correctly and return
// a slice of records to send.
func (req *FCGIRequest) EncodeContext() []Record {
	records := []Record{}
	for k, v := range req.Context {
		// We'll use this to put together
		// the body of the record
		var buf bytes.Buffer

		// Let's see how many bytes we have in total.
		// Since we have to leave 8 bytes for encoding
		// the sizes, we'll add it to the calculation.
		// If the value is larger than what we can
		// handle, we'll truncate it.
		if (8 + len(k) + len(v)) > maxWrite {
			valMaxLength := maxWrite - 8 - len(k)
			v = v[:valMaxLength]
		}

		// The high bit of name size and value size is used for signaling
		// how many bytes are used to store the length/size.
		// If the size is > 127, we can just use one byte,
		// and the high bit will be 0, otherwise, we use
		// four bytes and the high bit will be 1
		// So if length is encoded in 4 bytes it would look
		// something like:
		// 10000000000000000000010000100000
		// For lengths < 127, we just use
		// one byte with a high bit of 0
		// 01001001
		if len(k) > 127 {
			size := uint32(len(k))
			size |= 1 << 31 // Set the high bit to 1
			b := make([]byte, 4)
			binary.BigEndian.PutUint32(b, size)
			buf.Write(b)
		} else {
			buf.Write([]byte{byte(len(k))})
		}

		if len(v) > 127 {
			size := uint32(len(v))
			size |= 1 << 31
			b := make([]byte, 4)
			binary.BigEndian.PutUint32(b, size)
			buf.Write(b)
		} else {
			buf.Write([]byte{byte(len(v))})
		}

		// Now we just write our values to the buffer
		buf.WriteString(k)
		buf.WriteString(v)

		records = append(records, *req.NewRecord(FCGI_PARAMS, buf.Bytes()))
		buf.Reset()
	}

	return records
}
