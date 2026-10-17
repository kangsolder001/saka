package mail_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appmail "github.com/kangsolder001/saka/internal/mail"
	"github.com/kangsolder001/saka/internal/platform/clock"
)

// fakeSMTP is a minimal in-process SMTP server that records what it receives.
type fakeSMTP struct {
	ln net.Listener
	wg sync.WaitGroup

	mu        sync.Mutex
	mailFrom  string
	rcptTo    []string
	data      string
	rcptReply string // overrides the RCPT TO reply, e.g. "550 no such user"
	hang      bool   // never answers after the greeting
}

func newFakeSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	s := &fakeSMTP{ln: ln}
	s.wg.Add(1)
	go s.serve()
	t.Cleanup(func() {
		_ = ln.Close()
		s.wg.Wait()
	})
	return s
}

func (s *fakeSMTP) setRcptReply(reply string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rcptReply = reply
}

func (s *fakeSMTP) setHang() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hang = true
}

func (s *fakeSMTP) port() int { return s.ln.Addr().(*net.TCPAddr).Port }

func (s *fakeSMTP) serve() {
	defer s.wg.Done()
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer func() { _ = conn.Close() }()
			s.handle(conn)
		}()
	}
}

func (s *fakeSMTP) handle(conn net.Conn) {
	tp := textproto.NewConn(conn)
	_ = tp.PrintfLine("220 fake ESMTP")
	s.mu.Lock()
	hang := s.hang
	s.mu.Unlock()
	if hang {
		_, _ = io.Copy(io.Discard, conn) // hold the connection until the client gives up
		return
	}
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}
		cmd := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			_ = tp.PrintfLine("250 fake")
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			s.mu.Lock()
			s.mailFrom = line[len("MAIL FROM:"):]
			s.mu.Unlock()
			_ = tp.PrintfLine("250 ok")
		case strings.HasPrefix(cmd, "RCPT TO:"):
			s.mu.Lock()
			reply := s.rcptReply
			s.rcptTo = append(s.rcptTo, line[len("RCPT TO:"):])
			s.mu.Unlock()
			if reply != "" {
				_ = tp.PrintfLine("%s", reply)
				continue
			}
			_ = tp.PrintfLine("250 ok")
		case cmd == "DATA":
			_ = tp.PrintfLine("354 go ahead")
			body, err := tp.ReadDotBytes()
			if err != nil {
				return
			}
			s.mu.Lock()
			s.data = string(body)
			s.mu.Unlock()
			_ = tp.PrintfLine("250 queued")
		case cmd == "QUIT":
			_ = tp.PrintfLine("221 bye")
			return
		default:
			_ = tp.PrintfLine("502 unsupported")
		}
	}
}

func (s *fakeSMTP) received() (from string, rcpt []string, data string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mailFrom, append([]string(nil), s.rcptTo...), s.data
}

func newMailer(t *testing.T, port int) *appmail.SMTPMailer {
	t.Helper()
	m, err := appmail.NewSMTPMailer(appmail.SMTPConfig{
		Host: "127.0.0.1", Port: port, From: "Saka <noreply@saka.test>",
	}, clock.NewFake(time.Date(2026, 3, 1, 9, 30, 0, 0, time.UTC)))
	require.NoError(t, err)
	return m
}

func TestNewSMTPMailer_Validation(t *testing.T) {
	clk := clock.NewFake(time.Now())
	_, err := appmail.NewSMTPMailer(appmail.SMTPConfig{Port: 25, From: "a@b.co"}, clk)
	assert.Error(t, err, "host required")
	_, err = appmail.NewSMTPMailer(appmail.SMTPConfig{Host: "h", From: "a@b.co"}, clk)
	assert.Error(t, err, "port required")
	_, err = appmail.NewSMTPMailer(appmail.SMTPConfig{Host: "h", Port: 25, From: "bad"}, clk)
	assert.Error(t, err, "from must be an address")
}

func TestSMTPMailer_DeliversMultipartMessage(t *testing.T) {
	srv := newFakeSMTP(t)
	m := newMailer(t, srv.port())

	err := m.Send(context.Background(), appmail.Message{
		To:      "Ann <ann@example.com>",
		Subject: "Verifikasi email Anda — Saka",
		Text:    "Halo Ann,\nklik tautan ini.",
		HTML:    "<p>Halo <b>Ann</b>, café</p>",
	})

	require.NoError(t, err)
	from, rcpt, data := srv.received()
	assert.Equal(t, "<noreply@saka.test>", from)
	assert.Equal(t, []string{"<ann@example.com>"}, rcpt)

	msg, err := mail.ReadMessage(strings.NewReader(strings.ReplaceAll(data, "\r\n", "\n")))
	require.NoError(t, err)
	assert.Equal(t, `"Saka" <noreply@saka.test>`, msg.Header.Get("From"))
	assert.Equal(t, "Ann <ann@example.com>", msg.Header.Get("To"))
	assert.Equal(t, "Sun, 01 Mar 2026 09:30:00 +0000", msg.Header.Get("Date"))
	assert.Regexp(t, `^<[0-9a-f-]{36}@saka\.test>$`, msg.Header.Get("Message-ID"))
	assert.Equal(t, "1.0", msg.Header.Get("MIME-Version"))

	subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	require.NoError(t, err)
	assert.Equal(t, "Verifikasi email Anda — Saka", subject, "non-ASCII subject survives RFC 2047 encoding")

	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	require.NoError(t, err)
	assert.Equal(t, "multipart/alternative", mediaType)

	parts := readParts(t, msg.Body, params["boundary"])
	require.Len(t, parts, 2)
	assert.Equal(t, "text/plain; charset=utf-8", parts[0].contentType)
	assert.Contains(t, parts[0].body, "Halo Ann,\nklik tautan ini.")
	assert.Equal(t, "text/html; charset=utf-8", parts[1].contentType, "HTML last: clients prefer the last alternative")
	assert.Contains(t, parts[1].body, "café", "UTF-8 body decodes from quoted-printable")
}

type part struct{ contentType, body string }

func readParts(t *testing.T, body io.Reader, boundary string) []part {
	t.Helper()
	mr := multipart.NewReader(body, boundary)
	var out []part
	for {
		p, err := mr.NextPart() // decodes quoted-printable automatically
		if errors.Is(err, io.EOF) {
			return out
		}
		require.NoError(t, err)
		b, err := io.ReadAll(p)
		require.NoError(t, err)
		out = append(out, part{contentType: p.Header.Get("Content-Type"), body: strings.ReplaceAll(string(b), "\r\n", "\n")})
	}
}

func TestSMTPMailer_TextOnlyMessage(t *testing.T) {
	srv := newFakeSMTP(t)
	m := newMailer(t, srv.port())

	require.NoError(t, m.Send(context.Background(), appmail.Message{To: "a@example.com", Subject: "s", Text: "only text"}))

	_, _, data := srv.received()
	assert.Contains(t, data, "text/plain")
	assert.NotContains(t, data, "text/html")
}

func TestSMTPMailer_RejectsInvalidMessageBeforeConnecting(t *testing.T) {
	// Port 1 is closed: the test fails with a connection error if validation does not run first.
	m := newMailer(t, 1)

	err := m.Send(context.Background(), appmail.Message{To: "bad", Subject: "s", Text: "t"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid recipient")
}

func TestSMTPMailer_RecipientRejectedIsPermanent(t *testing.T) {
	srv := newFakeSMTP(t)
	srv.setRcptReply("550 5.1.1 no such user")
	m := newMailer(t, srv.port())

	err := m.Send(context.Background(), appmail.Message{To: "ghost@example.com", Subject: "s", Text: "t"})

	require.Error(t, err)
	assert.True(t, appmail.IsPermanentSMTPError(err), "unknown mailbox should not be retried: %v", err)
}

func TestSMTPMailer_TemporaryRejectionIsRetryable(t *testing.T) {
	srv := newFakeSMTP(t)
	srv.setRcptReply("451 4.3.0 try later")
	m := newMailer(t, srv.port())

	err := m.Send(context.Background(), appmail.Message{To: "a@example.com", Subject: "s", Text: "t"})

	require.Error(t, err)
	assert.False(t, appmail.IsPermanentSMTPError(err))
}

func TestIsPermanentSMTPError(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{&textproto.Error{Code: 550}, true},
		{&textproto.Error{Code: 554}, true},
		{&textproto.Error{Code: 535}, false},
		{&textproto.Error{Code: 530}, false},
		{&textproto.Error{Code: 534}, false},
		{&textproto.Error{Code: 421}, false},
		{&textproto.Error{Code: 451}, false},
		{fmt.Errorf("wrapped: %w", &textproto.Error{Code: 553}), true},
		{errors.New("connection refused"), false},
		{nil, false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, appmail.IsPermanentSMTPError(tt.err), "%v", tt.err)
	}
}

func TestSMTPMailer_ConnectionRefused(t *testing.T) {
	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	require.NoError(t, ln.Close()) // nothing listens on this port any more
	m := newMailer(t, port)

	err = m.Send(context.Background(), appmail.Message{To: "a@example.com", Subject: "s", Text: "t"})

	require.Error(t, err)
	assert.False(t, appmail.IsPermanentSMTPError(err), "an unreachable server is transient")
}

func TestSMTPMailer_HonoursContextDeadlineWhenServerHangs(t *testing.T) {
	srv := newFakeSMTP(t)
	srv.setHang()
	m := newMailer(t, srv.port())
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := m.Send(ctx, appmail.Message{To: "a@example.com", Subject: "s", Text: "t"})

	require.Error(t, err)
	assert.Less(t, time.Since(start), 2*time.Second, "must give up shortly after the deadline")
}
