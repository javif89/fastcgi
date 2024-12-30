package fastcgi

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/textproto"
	"os"
	"strconv"
	"strings"
	"sync"
)

type FCGIClient struct {
	mutex     sync.Mutex
	rwc       io.ReadWriteCloser
	h         Header
	buf       bytes.Buffer
	keepAlive bool
	reqId     uint16
}

// Do made the request and returns a io.Reader that translates the data read
// from fcgi responder out of fcgi packet before returning it.
func (client *FCGIClient) Forward(r *http.Request, root string, script string) (http.Response, error) {
	req := RequestFromHttp(r)
	req.Root(root)
	req.Script(script)
	log.Printf("______REQUEST %d______", req.Id)

	log.Println("Begin request")
	client.beginRequest(req)

	// Write the request context as a stream
	log.Println("Sending FCGI_PARAMS")
	client.writeStream(req, req.EncodeContext())
	log.Println("Done")

	// Write the body (if any)
	body := req.EncodeBody()
	if len(body) > 0 {
		log.Println("Sending body")
		client.writeStream(req, body)
	}

	// Read the app response from the FCGI_STDOUT stream
	log.Println("Reading response")
	respContent := client.readResponse()

	log.Printf("______END REQUEST %d______", req.Id)

	f, _ := os.Create("./resp.txt")
	defer f.Close()
	f.Write(respContent)

	return parseHttp(respContent)
}

// Close fcgi connnection
func (client *FCGIClient) Close() {
	client.rwc.Close()
}

func (c *FCGIClient) beginRequest(req *FCGIRequest) error {
	err := c.writeRecord(req.NewBeginRequestRecord())
	if err != nil {
		return err
	}

	return nil
}

func (client *FCGIClient) writeRecord(r *Record) (err error) {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	client.buf.Reset()

	// Write the record to the connection
	b, err := r.toBytes()
	_, err = client.rwc.Write(b)
	return err
}

// We write long data such as FCGI_PARAMS or FCGI_STDIN
// as a stream. Sending an empty record of the same
// type to signal the end.
func (c *FCGIClient) writeStream(req *FCGIRequest, records []Record) error {
	if len(records) == 0 {
		return nil
	}

	for _, r := range records {
		c.writeRecord(&r)
	}

	// Send an empty record to end the stream.
	// all the records should be of the same
	// type so we'll just use the type from
	// the first item in the slice.
	end := req.NewRecord(records[0].Header.Type, nil)
	c.writeRecord(end)
	return nil
}

func readRecord(r io.Reader) (*Record, error) {
	// It's easier to read the header piece of the record
	// into the struct as opposed to doing it piece by
	// piece. But we'll do it this way to be explicit.
	// Just know that you could also do:
	// h := Header{}
	// binary.Read(r, binary.BigEndian, &h)
	var version uint8
	var recType FCGIRecordType
	var id uint16
	var contentlength uint16
	var paddinglength uint8
	var reserved uint8

	// Let's read the header fields of the record.
	binary.Read(r, binary.BigEndian, &version)
	binary.Read(r, binary.BigEndian, &recType)
	binary.Read(r, binary.BigEndian, &id)
	binary.Read(r, binary.BigEndian, &contentlength)
	binary.Read(r, binary.BigEndian, &paddinglength)
	binary.Read(r, binary.BigEndian, &reserved)

	readLength := int(contentlength) + int(paddinglength)
	content := make([]byte, readLength)

	if _, err := io.ReadFull(r, content); err != nil {
		return nil, err
	}

	// Remove any padding from the content
	content = content[:contentlength]

	rec := Record{}
	rec.Header.Version = version
	rec.Header.Type = recType
	rec.Header.Id = id
	rec.Header.ContentLength = contentlength
	rec.Header.PaddingLength = paddinglength
	rec.Header.Reserved = reserved
	rec.Content = content

	return &rec, nil
}

func (c *FCGIClient) readResponse() []byte {
	var response bytes.Buffer

	for {
		r, err := readRecord(c.rwc)

		if err != nil {
			log.Printf("Encountered error when reading the stream: %s", err.Error())
		}

		if r.Header.Type == FCGI_END_REQUEST {
			break
		}

		response.Write(r.Content)
	}

	return response.Bytes()
}

func parseHttp(raw []byte) (http.Response, error) {
	log.Println("Parsing http")
	bf := bufio.NewReader(bytes.NewReader(raw))
	tp := textproto.NewReader(bf)
	resp := new(http.Response)
	// Ensure we have a valid http response
	line, err := tp.ReadLine()
	if err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return http.Response{}, err
	}

	i := strings.IndexByte(line, ' ')

	if i == -1 {
		return http.Response{}, &badStringError{"malformed HTTP response", line}
	}

	resp.Proto = line[:i]
	resp.Status = strings.TrimLeft(line[i+1:], " ")
	statusCode := resp.Status
	if i := strings.IndexByte(resp.Status, ' '); i != -1 {
		statusCode = resp.Status[:i]
	}
	if len(statusCode) != 3 {
		err = &badStringError{"malformed HTTP status code", statusCode}
	}
	resp.StatusCode, err = strconv.Atoi(statusCode)
	if err != nil || resp.StatusCode < 0 {
		err = &badStringError{"malformed HTTP status code", statusCode}
	}
	var ok bool
	if resp.ProtoMajor, resp.ProtoMinor, ok = http.ParseHTTPVersion(resp.Proto); !ok {
		err = &badStringError{"malformed HTTP version", resp.Proto}
	}
	// Parse the response headers.
	mimeHeader, err := tp.ReadMIMEHeader()
	if err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return http.Response{}, err
	}
	resp.Header = http.Header(mimeHeader)
	resp.TransferEncoding = resp.Header["Transfer-Encoding"]
	resp.ContentLength, _ = strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)

	if chunked(resp.TransferEncoding) {
		resp.Body = io.NopCloser(httputil.NewChunkedReader(bf))
	} else {
		resp.Body = io.NopCloser(bf)
	}

	log.Println("Done")

	return *resp, nil
}
