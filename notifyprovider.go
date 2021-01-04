package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/smtp"
	"net/url"
	"time"
)

type Notify struct {
	conf *Conf
}

func (n *Notify) NotifyEmail() error {
	if n.conf.Email == nil {
		return errors.New("не заданы настройки email")
	}
	auth := smtp.PlainAuth("", n.conf.Email.UserName, n.conf.Email.Pass, n.conf.Email.SMTP)

	header := make(map[string]string)
	header["From"] = n.conf.Email.UserName
	header["Subject"] = n.conf.Email.Subject
	header["MIME-Version"] = "1.0"
	header["Content-Type"] = "text/plain; charset=\"utf-8\""
	header["Content-Transfer-Encoding"] = "base64"

	message := ""
	for k, v := range header {
		message += fmt.Sprintf("%s: %s\r\n", k, v)
	}
	message += "\r\n" + base64.StdEncoding.EncodeToString([]byte(n.conf.Msgtxt))

	err := smtp.SendMail( n.conf.Email.SMTP + ":587", auth, n.conf.Email.UserName, n.conf.Email.Recipients, []byte(message))
	if err != nil {
		return err
	}
	return nil
}

func (n *Notify) NotifyTelegram() error {
	client := new(http.Client)
	client.Timeout = time.Minute*5

	for _, rec := range n.conf.Telegram.Recipients {
		url := fmt.Sprintf("%s?userid=%s&msg=%s", n.conf.Telegram.URL, rec, url.QueryEscape(n.conf.Msgtxt))
		if resp, err := client.Get(url); err != nil {
			return err
		} else if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("StatusCode eq %d", resp.StatusCode)
		}
	}

	return nil
}