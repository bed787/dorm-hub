package mail

import (
	"context"
	"crypto/tls"
	"dorm-hub/internal/config"
	"fmt"
	"net"
	stdmail "net/mail"
	"net/smtp"
	"time"
)

type Sender interface {
	SendCode(context.Context, string, string) error
}
type SMTP struct{ cfg config.Config }

func New(cfg config.Config) *SMTP { return &SMTP{cfg: cfg} }
func (s *SMTP) SendCode(ctx context.Context, to, code string) error {
	return s.send(ctx, to, "Dorm Hub login code", "Ваш код входа в Dorm Hub", code)
}
func (s *SMTP) SendDeletionCode(ctx context.Context, to, code string) error {
	return s.send(ctx, to, "Dorm Hub account deletion", "Код для полного удаления аккаунта Dorm Hub. Удаление необратимо. Введите код только если хотите удалить аккаунт", code)
}
func (s *SMTP) send(ctx context.Context, to, subject, purpose, code string) error {
	recipient, err := stdmail.ParseAddress(to)
	if err != nil {
		return err
	}
	from, err := stdmail.ParseAddress(s.cfg.SMTPFrom)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(10 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", net.JoinHostPort(s.cfg.SMTPHost, s.cfg.SMTPPort))
	if err != nil {
		return err
	}
	defer conn.Close()
	conn.SetDeadline(deadline)
	client, err := smtp.NewClient(conn, s.cfg.SMTPHost)
	if err != nil {
		return err
	}
	defer client.Close()
	if s.cfg.SMTPStartTLS {
		if err = client.StartTLS(&tls.Config{ServerName: s.cfg.SMTPHost, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	}
	if s.cfg.SMTPUser != "" {
		if err = client.Auth(smtp.PlainAuth("", s.cfg.SMTPUser, s.cfg.SMTPPassword, s.cfg.SMTPHost)); err != nil {
			return err
		}
	}
	if err = client.Mail(from.Address); err != nil {
		return err
	}
	if err = client.Rcpt(recipient.Address); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	message := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s: %s\r\nКод действует 10 минут. Если вы не запрашивали этот код, проигнорируйте письмо.\r\n", from.String(), recipient.String(), subject, purpose, code)
	if _, err = writer.Write([]byte(message)); err != nil {
		return err
	}
	if err = writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}
