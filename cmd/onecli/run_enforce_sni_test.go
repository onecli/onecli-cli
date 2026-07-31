package main

// Tests for SNI extraction. The critical one is
// TestParseSNIAgainstRealClientHello: it feeds bytes produced by Go's own
// crypto/tls, so the parser is validated against a real implementation
// rather than a hand-built fixture that could share my misreading of the
// spec.

import (
	"crypto/tls"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

// captureClientHello starts a listener, points a real TLS client at it, and
// returns the raw first flight the client sent.
func captureClientHello(t *testing.T, serverName string) []byte {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	type result struct {
		buf []byte
		err error
	}
	done := make(chan result, 1)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			done <- result{err: err}
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		buf := make([]byte, 4096)
		n, err := conn.Read(buf)
		if err != nil {
			done <- result{err: err}
			return
		}
		done <- result{buf: buf[:n]}
	}()

	// The handshake will fail (no server cert); we only need the hello.
	c, err := net.DialTimeout("tcp", ln.Addr().String(), 5*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	tlsConn := tls.Client(c, &tls.Config{ServerName: serverName})
	_ = tlsConn.SetDeadline(time.Now().Add(2 * time.Second))
	_ = tlsConn.Handshake() // expected to fail
	_ = tlsConn.Close()

	r := <-done
	if r.err != nil {
		t.Fatalf("capturing hello: %v", r.err)
	}
	return r.buf
}

func TestParseSNIAgainstRealClientHello(t *testing.T) {
	for _, host := range []string{
		"api.anthropic.com",
		"agentn.global.api5.cursor.sh",
		"a.co",
		// Long-but-legal name: exercises multi-byte vector lengths.
		strings.Repeat("sub.", 40) + "example.com",
	} {
		t.Run(host, func(t *testing.T) {
			hello := captureClientHello(t, host)
			got, err := parseSNI(hello)
			if err != nil {
				t.Fatalf("parseSNI on a real hello for %q: %v", host, err)
			}
			if got != host {
				t.Fatalf("parseSNI = %q, want %q", got, host)
			}
		})
	}
}

// TestParseSNIPartialRecord feeds the hello one byte at a time and requires
// errHelloPartial until the record is complete. A parser that guessed early
// would route traffic based on a truncated name.
func TestParseSNIPartialRecord(t *testing.T) {
	const host = "api.anthropic.com"
	hello := captureClientHello(t, host)

	for n := 0; n < len(hello); n++ {
		_, err := parseSNI(hello[:n])
		if !errors.Is(err, errHelloPartial) {
			t.Fatalf("parseSNI on %d/%d bytes: got %v, want errHelloPartial",
				n, len(hello), err)
		}
	}
	got, err := parseSNI(hello)
	if err != nil || got != host {
		t.Fatalf("parseSNI on the complete hello = %q, %v", got, err)
	}
}

func TestParseSNIRejectsNonTLS(t *testing.T) {
	for name, input := range map[string][]byte{
		"http request":  []byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"),
		"ssh banner":    []byte("SSH-2.0-OpenSSH_9.0\r\n"),
		"random binary": {0xde, 0xad, 0xbe, 0xef, 0x00, 0x11, 0x22},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseSNI(input); !errors.Is(err, errNotTLS) {
				t.Fatalf("got %v, want errNotTLS", err)
			}
		})
	}
}

// TestParseSNIRejectsHeaderInjection is the security-critical case: the SNI
// is attacker-controlled and ends up in a CONNECT request line. A CRLF must
// never survive parsing.
func TestParseSNIRejectsHeaderInjection(t *testing.T) {
	for name, host := range map[string]string{
		"crlf":        "evil.com\r\nX-Injected: 1",
		"lf only":     "evil.com\nX-Injected: 1",
		"space":       "evil.com X-Injected",
		"null byte":   "evil.com\x00",
		"colon port":  "evil.com:1234",
		"slash path":  "evil.com/path",
		"leading dot": ".evil.com",
		"double dot":  "evil..com",
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateSNIHost(host); err == nil {
				t.Fatalf("validateSNIHost(%q) accepted an unsafe hostname", host)
			}
		})
	}
}

func TestValidateSNIHostAcceptsRealNames(t *testing.T) {
	for _, host := range []string{
		"api.anthropic.com",
		"agentn.global.api5.cursor.sh",
		"a.co",
		"my-host_1.example.com",
		"localhost",
	} {
		if err := validateSNIHost(host); err != nil {
			t.Fatalf("validateSNIHost(%q) rejected a legitimate name: %v", host, err)
		}
	}
}

// TestParseSNIFuzzDoesNotPanic feeds truncations and byte-flips of a real
// hello. The parser must always return an error, never panic: it runs on
// input from the very process we are sandboxing.
func TestParseSNIFuzzDoesNotPanic(t *testing.T) {
	hello := captureClientHello(t, "api.anthropic.com")

	for i := 0; i < len(hello); i++ {
		for _, flip := range []byte{0x00, 0xff, 0x7f} {
			mutated := make([]byte, len(hello))
			copy(mutated, hello)
			mutated[i] = flip
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("panic on byte %d flipped to %#x: %v", i, flip, r)
					}
				}()
				_, _ = parseSNI(mutated)
			}()
		}
	}
}

func TestParseSNIRejectsOversizedRecord(t *testing.T) {
	// Record header claiming a length beyond the TLS maximum.
	buf := []byte{tlsRecordTypeHandshake, 0x03, 0x03, 0xff, 0xff}
	if _, err := parseSNI(buf); !errors.Is(err, errHelloTooBig) {
		t.Fatalf("got %v, want errHelloTooBig", err)
	}
}
