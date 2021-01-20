package main

import (
	"context"
	"github.com/clintjedwards/avail"
	"github.com/matryer/resync"
	"time"
)

type Conf struct {
	// Cron шаблон
	Cron *struct {
		Patteren string
		TimeZone string
	}

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
	once     resync.Once
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

	avail, _ := avail.New(this.conf.Cron.Patteren)
	loc, _ := time.LoadLocation(this.conf.Cron.TimeZone)

B:
	for {
		select {
		case <-this.tick.C:
			if avail.Able(time.Now().In(loc)) {
				this.once.Do(this.callback) // once.Do нужен что б не выполнялось каждую секунду
			} else {
				this.once.Reset()
			}

		case <-this.ctx.Done():
			break B
		}
	}

	return true
}
