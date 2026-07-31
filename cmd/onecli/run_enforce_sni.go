package main

// TLS ClientHello SNI extraction for transparent-redirect mode.
//
// Why this exists: under transparent redirection an app dials a real host
// directly and pf rewrites the destination to our loopback listener. The
// connection therefore arrives with NO protocol-level statement of where it
// was going — the original destination lives only in pf's state table.
//
// The obvious way to recover it is the DIOCNATLOOK ioctl on the pf device,
// but that requires root on EVERY connection. Parsing the SNI instead needs
// no privilege at all, and the hostname is exactly what the gateway's
// CONNECT needs. Root stays confined to loading the pf anchor once at
// session start, never in the data path.
//
// Deliberately hand-rolled rather than using crypto/tls: we must inspect the
// ClientHello WITHOUT terminating TLS (the gateway does that), and Go's
// stdlib offers no "peek at the handshake" primitive that leaves the bytes
// replayable.
//
// Fails CLOSED: any malformed, truncated, non-TLS, or SNI-less hello returns
// an error and the connection is refused. Guessing a destination would mean
// sending an agent's traffic somewhere it did not ask for.

import (
	"errors"
	"fmt"
	"strings"
)

var (
	errNotTLS       = errors.New("not a TLS ClientHello")
	errNoSNI        = errors.New("ClientHello carries no SNI extension")
	errHelloTooBig  = errors.New("ClientHello exceeds the maximum record size")
	errHelloPartial = errors.New("ClientHello is incomplete")
)

const (
	// TLS record layer.
	tlsRecordTypeHandshake = 0x16
	tlsHandshakeClientHello = 0x01
	tlsExtensionServerName  = 0x0000
	sniTypeHostName         = 0x00

	// A ClientHello must fit one record. 16KB is the TLS maximum plaintext
	// fragment; anything larger is not a hello we can use.
	maxClientHelloSize = 16 * 1024
	// Minimum bytes needed before the record length is knowable.
	tlsRecordHeaderLen = 5
)

// parseSNI extracts the server_name from a complete TLS ClientHello.
//
// The input must begin at the record header. Returns errHelloPartial if buf
// does not yet hold the whole record, which lets the caller read more rather
// than fail — the hello can legitimately arrive split across TCP segments.
func parseSNI(buf []byte) (string, error) {
	if len(buf) < tlsRecordHeaderLen {
		return "", errHelloPartial
	}
	if buf[0] != tlsRecordTypeHandshake {
		// Not a handshake record. Could be plain HTTP or another protocol;
		// either way we cannot recover a destination from it.
		return "", errNotTLS
	}
	// buf[1:3] is the record-layer version. Deliberately NOT validated:
	// TLS 1.3 sends a legacy 0x0303 here and middleboxes vary. The
	// handshake type below is the reliable discriminator.
	recordLen := int(buf[3])<<8 | int(buf[4])
	if recordLen > maxClientHelloSize {
		return "", errHelloTooBig
	}
	if len(buf) < tlsRecordHeaderLen+recordLen {
		return "", errHelloPartial
	}
	body := buf[tlsRecordHeaderLen : tlsRecordHeaderLen+recordLen]

	return parseClientHelloBody(body)
}

// parseClientHelloBody walks the handshake message. Every step is
// length-checked: this parses attacker-influenced bytes from a process we
// are sandboxing precisely because we do not trust it.
func parseClientHelloBody(b []byte) (string, error) {
	r := &byteReader{buf: b}

	msgType, ok := r.u8()
	if !ok {
		return "", errHelloPartial
	}
	if msgType != tlsHandshakeClientHello {
		return "", errNotTLS
	}
	msgLen, ok := r.u24()
	if !ok {
		return "", errHelloPartial
	}
	// The handshake message may be shorter than the record (multiple
	// messages per record is legal); clamp to it.
	body, ok := r.take(int(msgLen))
	if !ok {
		return "", errHelloPartial
	}
	r = &byteReader{buf: body}

	if _, ok := r.take(2); !ok { // client_version
		return "", errHelloPartial
	}
	if _, ok := r.take(32); !ok { // random
		return "", errHelloPartial
	}
	if _, ok := r.vector8(); !ok { // legacy_session_id
		return "", errHelloPartial
	}
	if _, ok := r.vector16(); !ok { // cipher_suites
		return "", errHelloPartial
	}
	if _, ok := r.vector8(); !ok { // compression_methods
		return "", errHelloPartial
	}

	exts, ok := r.vector16()
	if !ok {
		// No extensions block at all: legal in ancient TLS, but then there
		// is no SNI and we cannot route.
		return "", errNoSNI
	}
	er := &byteReader{buf: exts}
	for er.remaining() > 0 {
		extType, ok := er.u16()
		if !ok {
			return "", errHelloPartial
		}
		extData, ok := er.vector16()
		if !ok {
			return "", errHelloPartial
		}
		if extType != tlsExtensionServerName {
			continue
		}
		return parseServerNameExtension(extData)
	}
	return "", errNoSNI
}

// parseServerNameExtension reads a ServerNameList and returns the first
// host_name entry.
func parseServerNameExtension(b []byte) (string, error) {
	r := &byteReader{buf: b}
	list, ok := r.vector16()
	if !ok {
		return "", errHelloPartial
	}
	lr := &byteReader{buf: list}
	for lr.remaining() > 0 {
		nameType, ok := lr.u8()
		if !ok {
			return "", errHelloPartial
		}
		name, ok := lr.vector16()
		if !ok {
			return "", errHelloPartial
		}
		if nameType != sniTypeHostName {
			continue
		}
		host := string(name)
		if err := validateSNIHost(host); err != nil {
			return "", err
		}
		return host, nil
	}
	return "", errNoSNI
}

// validateSNIHost rejects hostnames we refuse to put in a CONNECT line.
//
// This is a security boundary, not hygiene: the value is attacker-controlled
// and gets interpolated into an HTTP request line sent upstream. A embedded
// CRLF would let a sandboxed process forge arbitrary headers on the gateway
// connection — including its own Proxy-Authorization. Fail closed on
// anything that is not a plausible DNS name.
func validateSNIHost(h string) error {
	if h == "" {
		return errNoSNI
	}
	if len(h) > 253 {
		return fmt.Errorf("SNI host exceeds the DNS length limit")
	}
	for i := 0; i < len(h); i++ {
		c := h[i]
		isAlnum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if isAlnum || c == '-' || c == '.' || c == '_' {
			continue
		}
		return fmt.Errorf("SNI host contains an illegal byte %q", c)
	}
	// A leading dot or a doubled dot is not a resolvable name and suggests
	// a crafted value.
	if strings.HasPrefix(h, ".") || strings.Contains(h, "..") {
		return fmt.Errorf("SNI host is not a well-formed DNS name")
	}
	return nil
}

// byteReader is a bounds-checked cursor. Every read returns ok=false rather
// than panicking, so malformed input becomes a refused connection.
type byteReader struct {
	buf []byte
	pos int
}

func (r *byteReader) remaining() int { return len(r.buf) - r.pos }

func (r *byteReader) take(n int) ([]byte, bool) {
	if n < 0 || r.remaining() < n {
		return nil, false
	}
	out := r.buf[r.pos : r.pos+n]
	r.pos += n
	return out, true
}

func (r *byteReader) u8() (uint8, bool) {
	b, ok := r.take(1)
	if !ok {
		return 0, false
	}
	return b[0], true
}

func (r *byteReader) u16() (uint16, bool) {
	b, ok := r.take(2)
	if !ok {
		return 0, false
	}
	return uint16(b[0])<<8 | uint16(b[1]), true
}

func (r *byteReader) u24() (uint32, bool) {
	b, ok := r.take(3)
	if !ok {
		return 0, false
	}
	return uint32(b[0])<<16 | uint32(b[1])<<8 | uint32(b[2]), true
}

// vector8 reads a length-prefixed vector with a 1-byte length.
func (r *byteReader) vector8() ([]byte, bool) {
	n, ok := r.u8()
	if !ok {
		return nil, false
	}
	return r.take(int(n))
}

// vector16 reads a length-prefixed vector with a 2-byte length.
func (r *byteReader) vector16() ([]byte, bool) {
	n, ok := r.u16()
	if !ok {
		return nil, false
	}
	return r.take(int(n))
}
