package notifier

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/smtp"
	"time"
)

// sendSMTPS 465 端口的隐式 TLS SMTP（net/smtp 的 SendMail 不支持直连 TLS）。
func sendSMTPS(addr, host string, auth smtp.Auth, from string, to []string, msg []byte) error {
	d := net.Dialer{Timeout: 15 * time.Second}
	conn, err := tls.DialWithDialer(&d, "tcp", addr, &tls.Config{ServerName: host})
	if err != nil {
		return err
	}
	defer conn.Close()
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer c.Close()
	if ok, _ := c.Extension("AUTH"); ok {
		if err := c.Auth(auth); err != nil {
			return err
		}
	}
	return smtpSend(c, from, to, msg)
}

// sendSMTPStartTLS 明文连接后升级 TLS（587 的标准方式）。
// 不用 smtp.SendMail：它遇到不支持 STARTTLS 的服务器会静默明文发送。
func sendSMTPStartTLS(ctx context.Context, addr, host string, auth smtp.Auth, from string, to []string, msg []byte) error {
	d := net.Dialer{Timeout: 15 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer c.Close()
	if ok, _ := c.Extension("STARTTLS"); !ok {
		return &PermanentError{Err: errors.New("smtp: 服务器不支持 STARTTLS，拒绝明文发送；请改用 465/SSL")}
	}
	if err := c.StartTLS(&tls.Config{ServerName: host}); err != nil {
		return err
	}
	if ok, _ := c.Extension("AUTH"); ok {
		if err := c.Auth(auth); err != nil {
			return err
		}
	}
	return smtpSend(c, from, to, msg)
}

// sendSMTPPlain 全程明文，仅在用户显式选择 none 时使用。
func sendSMTPPlain(ctx context.Context, addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
	d := net.Dialer{Timeout: 15 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	c, err := smtp.NewClient(conn, "")
	if err != nil {
		return err
	}
	defer c.Close()
	if ok, _ := c.Extension("AUTH"); ok {
		if err := c.Auth(auth); err != nil {
			return err
		}
	}
	return smtpSend(c, from, to, msg)
}

func smtpSend(c *smtp.Client, from string, to []string, msg []byte) error {
	if err := c.Mail(from); err != nil {
		return err
	}
	for _, rcpt := range to {
		if err := c.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// SendSystemMailRaw 系统邮件出口（注册验证码等非通知渠道邮件）。
// encryption: ssl | starttls | none；供 httpapi 的全局邮件配置复用，
// 与渠道投递共用同一套底层实现。
func SendSystemMailRaw(ctx context.Context, addr, host, encryption string, auth smtp.Auth, from string, to []string, msg []byte) error {
	switch encryption {
	case "ssl":
		return sendSMTPS(addr, host, auth, from, to, msg)
	case "none":
		return sendSMTPPlain(ctx, addr, auth, from, to, msg)
	default:
		return sendSMTPStartTLS(ctx, addr, host, auth, from, to, msg)
	}
}
