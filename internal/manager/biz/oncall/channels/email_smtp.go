package channels

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"html"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"sort"
	"strings"
	"time"
)

// sendSMTPEmail connects to the configured SMTP server (ssl / starttls / none) and delivers a multipart email.
func sendSMTPEmail(ctx context.Context, cfg *ChannelGatewayConfig, req *EmailRequest) (*EmailResult, error) {
	if cfg == nil || strings.TrimSpace(cfg.SMTPHost) == "" {
		return nil, errors.New("未配置 SMTP 邮件服务器地址 (smtp_host)")
	}
	host := sanitizeHeaderValue(cfg.SMTPHost)
	tlsMode := NormalizeSMTPTLSMode(cfg.SMTPTLSMode)
	port := NormalizeSMTPPort(cfg.SMTPPort, tlsMode)
	timeout := time.Duration(NormalizeSMTPTimeoutSeconds(cfg.SMTPTimeoutSeconds)) * time.Second

	fromRaw := strings.TrimSpace(cfg.SMTPFrom)
	if fromRaw == "" {
		fromRaw = strings.TrimSpace(cfg.SMTPUsername)
	}
	fromAddr, err := validateEmailAddress(fromRaw)
	if err != nil {
		return nil, fmt.Errorf("发件人邮箱地址无效: %w", err)
	}
	toAddr, err := validateEmailAddress(req.To)
	if err != nil {
		return nil, fmt.Errorf("收件人邮箱地址无效: %w", err)
	}

	subject := sanitizeHeaderValue(req.Subject)
	if subject == "" {
		subject = "[OpsPilot On-Call] 告警通知"
	}

	msgBytes := buildEmailPayload(fromRaw, toAddr.Address, subject, req)
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	deadline := time.Now().Add(timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}

	var conn net.Conn
	dialer := &net.Dialer{Timeout: timeout}
	tlsCfg := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if tlsMode == "ssl" {
		tlsDialer := &tls.Dialer{NetDialer: dialer, Config: tlsCfg}
		conn, err = tlsDialer.DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return failedEmailResult(toAddr.Address, subject, fmt.Errorf("连接 SMTP 服务器 %s 失败: %w", addr, err))
	}
	_ = conn.SetDeadline(deadline) // best-effort per-operation socket deadline bound to ctx

	client, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return failedEmailResult(toAddr.Address, subject, fmt.Errorf("SMTP 握手失败: %w", err))
	}
	defer func() { _ = client.Close() }() // fallback cleanup if Quit fails or returns early

	if tlsMode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return failedEmailResult(toAddr.Address, subject, errors.New("SMTP 服务器不支持 STARTTLS 加密"))
		}
		if err := client.StartTLS(tlsCfg); err != nil {
			return failedEmailResult(toAddr.Address, subject, fmt.Errorf("SMTP STARTTLS 升级失败: %w", err))
		}
	}

	if cfg.SMTPUsername != "" || cfg.SMTPPassword != "" {
		if err := client.Auth(smtp.PlainAuth("", cfg.SMTPUsername, cfg.SMTPPassword, host)); err != nil {
			return failedEmailResult(toAddr.Address, subject, fmt.Errorf("SMTP 身份认证失败: %w", err))
		}
	}
	if err := client.Mail(fromAddr.Address); err != nil {
		return failedEmailResult(toAddr.Address, subject, fmt.Errorf("SMTP MAIL FROM 失败: %w", err))
	}
	if err := client.Rcpt(toAddr.Address); err != nil {
		return failedEmailResult(toAddr.Address, subject, fmt.Errorf("SMTP RCPT TO 失败: %w", err))
	}

	wc, err := client.Data()
	if err != nil {
		return failedEmailResult(toAddr.Address, subject, fmt.Errorf("SMTP DATA 开启失败: %w", err))
	}
	if _, err := wc.Write(msgBytes); err != nil {
		_ = wc.Close()
		return failedEmailResult(toAddr.Address, subject, fmt.Errorf("写入邮件正文失败: %w", err))
	}
	if err := wc.Close(); err != nil {
		return failedEmailResult(toAddr.Address, subject, fmt.Errorf("提交邮件正文失败: %w", err))
	}
	if err := client.Quit(); err != nil {
		return failedEmailResult(toAddr.Address, subject, fmt.Errorf("SMTP 会话结束异常: %w", err))
	}

	return &EmailResult{
		Provider: "smtp",
		Status:   "sent",
		To:       toAddr.Address,
		Subject:  subject,
		Message:  fmt.Sprintf("邮件已通过 SMTP (%s) 成功发送至 %s", addr, toAddr.Address),
	}, nil
}

func failedEmailResult(to, subject string, err error) (*EmailResult, error) {
	return &EmailResult{
		Provider: "smtp",
		Status:   "failed",
		To:       to,
		Subject:  subject,
		Message:  err.Error(),
	}, err
}

func validateEmailAddress(raw string) (*mail.Address, error) {
	cleaned := sanitizeHeaderValue(raw)
	if cleaned == "" {
		return nil, errors.New("address is empty")
	}
	if strings.ContainsAny(raw, "\r\n\x00") {
		return nil, errors.New("address contains invalid control characters")
	}
	addr, err := mail.ParseAddress(cleaned)
	if err != nil {
		return nil, err
	}
	return addr, nil
}

func sanitizeHeaderValue(v string) string {
	v = strings.ReplaceAll(v, "\r", " ")
	v = strings.ReplaceAll(v, "\n", " ")
	v = strings.ReplaceAll(v, "\x00", "")
	return strings.TrimSpace(v)
}

func buildEmailPayload(fromHeader, toEmail, subject string, req *EmailRequest) []byte {
	boundary := fmt.Sprintf("OpsPilotOnCall_%d", time.Now().UnixNano())
	encodedSubject := mime.BEncoding.Encode("utf-8", subject)
	safeFrom := sanitizeHeaderValue(fromHeader)
	safeTo := sanitizeHeaderValue(toEmail)

	textBody := strings.TrimSpace(req.TextBody)
	if textBody == "" {
		textBody = subject
	}
	htmlBody := strings.TrimSpace(req.HTMLBody)
	if htmlBody == "" {
		htmlBody = renderDefaultHTMLBody(subject, textBody, req.ActionLinks)
	}

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "From: %s\r\n", safeFrom)
	fmt.Fprintf(&buf, "To: <%s>\r\n", safeTo)
	fmt.Fprintf(&buf, "Subject: %s\r\n", encodedSubject)
	fmt.Fprintf(&buf, "Date: %s\r\n", time.Now().UTC().Format(time.RFC1123Z))
	buf.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(&buf, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", boundary)

	fmt.Fprintf(&buf, "--%s\r\n", boundary)
	buf.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	buf.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	buf.WriteString(strings.ReplaceAll(textBody, "\r\n", "\n"))
	buf.WriteString("\r\n\r\n")

	fmt.Fprintf(&buf, "--%s\r\n", boundary)
	buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	buf.WriteString("Content-Transfer-Encoding: 8bit\r\n\r\n")
	buf.WriteString(htmlBody)
	buf.WriteString("\r\n\r\n")
	fmt.Fprintf(&buf, "--%s--\r\n", boundary)
	return buf.Bytes()
}

func renderDefaultHTMLBody(subject, textBody string, actionLinks map[string]string) string {
	var b strings.Builder
	b.WriteString(`<div style="font-family:system-ui,-apple-system,sans-serif;max-width:600px;margin:0 auto;padding:16px;border:1px solid #e5e7eb;border-radius:8px;">`)
	fmt.Fprintf(&b, `<h2 style="margin:0 0 12px;font-size:16px;color:#111827;">%s</h2>`, html.EscapeString(subject))
	escapedBody := strings.ReplaceAll(html.EscapeString(textBody), "\n", "<br/>")
	fmt.Fprintf(&b, `<p style="margin:0 0 16px;font-size:13px;line-height:1.6;color:#374151;">%s</p>`, escapedBody)

	if len(actionLinks) > 0 {
		keys := make([]string, 0, len(actionLinks))
		for k := range actionLinks {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteString(`<div style="display:flex;gap:8px;flex-wrap:wrap;">`)
		for _, k := range keys {
			u := strings.TrimSpace(actionLinks[k])
			if !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
				continue
			}
			fmt.Fprintf(&b, `<a href="%s" style="display:inline-block;padding:8px 14px;background:#4f46e5;color:#ffffff;text-decoration:none;border-radius:6px;font-size:12px;">%s</a> `,
				html.EscapeString(u), html.EscapeString(k))
		}
		b.WriteString(`</div>`)
	}
	b.WriteString(`</div>`)
	return b.String()
}
