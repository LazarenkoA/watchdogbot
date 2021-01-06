package main

import (
	"context"
	"time"
)

type Conf struct {
	// интервал через который будет срабатывать уведомление (в минутах)
	Interval int

	// Обратный отчет в секундах
	Timer int

	// Настройки отправки почты
	Email *struct {
		SMTP       string
		UserName   string
		Pass       string
		Subject    string
		Recipients []string
	}

	// текст сообщения
	Msgtxt string

	// Настройки отправки тележку, через rest API
	Telegram *struct {
		URL        string
		Recipients []string
	}
}

type scheduler struct {
	ctx      context.Context
	Cancel   context.CancelFunc
	tick     *time.Ticker
	conf     *Conf
	callback func()
}

func (this *scheduler) New(conf *Conf, callback func()) *scheduler {
	this.ctx, this.Cancel = context.WithCancel(context.Background())
	this.tick = time.NewTicker(time.Second)
	this.conf = conf
	this.callback = callback

	return this
}

func (this *scheduler) Invoke() bool {
	defer func() {
		this.tick.Stop()
	}()

	start := time.Now()
B:
	for {
		select {
		case <-this.tick.C:
			if time.Now().After(start.Add(time.Minute * time.Duration(this.conf.Interval))) {
				start = time.Now()
				this.callback()
			}

		case <-this.ctx.Done():
			break B
		}
	}

	return true
}
