package fastcgi

import (
	"fmt"
	"net"
)

func Connect(network, address string) (fcgi *FCGIClient, err error) {
	var conn net.Conn

	conn, err = net.Dial(network, address)
	if err != nil {
		return
	}

	fcgi = &FCGIClient{
		rwc:       conn,
		keepAlive: false,
	}

	return
}

type badStringError struct {
	what string
	str  string
}

func (e *badStringError) Error() string { return fmt.Sprintf("%s %q", e.what, e.str) }

// Checks whether chunked is part of the encodings stack
func chunked(te []string) bool { return len(te) > 0 && te[0] == "chunked" }
