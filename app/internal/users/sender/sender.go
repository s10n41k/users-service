package sender

import (
	"TODOLIST/app/internal/config"
	"fmt"
	"log"
	"net/smtp"
)

// EmailSender — интерфейс отправки писем в users-service
type EmailSender interface {
	SendPasswordResetCode(email, code string) error
}

type smtpSender struct {
	cfg config.Config
}

func NewEmailSender(cfg config.Config) EmailSender {
	return &smtpSender{cfg: cfg}
}

func (s *smtpSender) SendPasswordResetCode(email, code string) error {
	log.Printf("[USERS SMTP] Отправка кода сброса пароля на %s", email)
	log.Printf("\n\n🔑 КОД СБРОСА ПАРОЛЯ 🔑")
	log.Printf("📧 Email: %s", email)
	log.Printf("🔑 Код: %s", code)
	log.Printf("⏰ Действителен 10 минут\n\n")

	if s.cfg.SMTP.Host == "" || s.cfg.SMTP.Username == "" {
		log.Printf("[USERS SMTP] SMTP не настроен, код только в логах")
		return nil
	}

	boundary := "BOUNDARY_RESET_2026"
	textPlain := fmt.Sprintf("Ваш код для сброса пароля: %s\nДействителен 10 минут.", code)
	htmlBody := fmt.Sprintf(`<!DOCTYPE html>
<html><body style="font-family:sans-serif;background:#f5f5f5;padding:20px">
<div style="max-width:500px;margin:0 auto;background:white;border-radius:12px;padding:32px">
  <h2 style="color:#58CC02;margin:0 0 8px">Сброс пароля</h2>
  <p style="color:#666;margin:0 0 24px">Введите этот код на странице сброса пароля:</p>
  <div style="background:#f0fce8;border-radius:8px;padding:20px;text-align:center">
    <span style="font-size:36px;font-weight:900;letter-spacing:8px;color:#333">%s</span>
  </div>
  <p style="color:#999;font-size:12px;margin:20px 0 0">Код действителен 10 минут. Если вы не запрашивали сброс пароля — проигнорируйте это письмо.</p>
</div>
</body></html>`, code)

	msg := ""
	msg += fmt.Sprintf("From: Todolist <%s>\r\n", s.cfg.SMTP.Username)
	msg += fmt.Sprintf("To: %s\r\n", email)
	msg += "Subject: Todolist — сброс пароля\r\n"
	msg += "MIME-Version: 1.0\r\n"
	msg += fmt.Sprintf("Content-Type: multipart/alternative; boundary=%s\r\n", boundary)
	msg += "\r\n"
	msg += fmt.Sprintf("--%s\r\n", boundary)
	msg += "Content-Type: text/plain; charset=UTF-8\r\n\r\n"
	msg += textPlain + "\r\n"
	msg += fmt.Sprintf("--%s\r\n", boundary)
	msg += "Content-Type: text/html; charset=UTF-8\r\n\r\n"
	msg += htmlBody + "\r\n"
	msg += fmt.Sprintf("--%s--\r\n", boundary)

	smtpAddr := fmt.Sprintf("%s:%s", s.cfg.SMTP.Host, s.cfg.SMTP.Port)
	auth := smtp.PlainAuth("", s.cfg.SMTP.Username, s.cfg.SMTP.Password, s.cfg.SMTP.Host)

	err := smtp.SendMail(smtpAddr, auth, s.cfg.SMTP.Username, []string{email}, []byte(msg))
	if err != nil {
		log.Printf("[USERS SMTP] Ошибка отправки: %v (код доступен в логах)", err)
		return nil // не блокируем — код уже в логах
	}

	log.Printf("[USERS SMTP] Письмо отправлено на %s", email)
	return nil
}
