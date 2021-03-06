package main

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	uuid "github.com/nu7hatch/gouuid"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Button struct {
	caption string
	handler *func()
	timer   int
	ID      string
}

type Buttons []*Button

type TwatchDog struct {
	sync.Mutex

	bot *tgbotapi.BotAPI
	//chatIDs  map[int64]bool
	callback map[string]func()
	handlers map[int64]*scheduler
	running  int32
	//conf     map[int64]string
	r *Redis
}

func (this *TwatchDog) New() (result tgbotapi.UpdatesChannel, err error) {
	//this.chatIDs = map[int64]bool{}
	this.callback = map[string]func(){}
	this.handlers = map[int64]*scheduler{}
	//this.conf = map[int64]string{}
	this.r, _ = new(Redis).Create(redisaddr)

	this.bot, err = tgbotapi.NewBotAPIWithClient(BotToken, new(http.Client))
	//bot.Debug = true
	if err != nil {
		return nil, err
	}

	if WebhookURL == "" {
		WebhookURL = getngrokWebhookURL() // для запуска локально
		if WebhookURL == "" {
			return nil, errors.New("не удалось получить url WebhookURL")
		}
	}

	_, err = this.bot.SetWebhook(tgbotapi.NewWebhook(WebhookURL))
	if err != nil {
		return nil, err
	}

	go http.ListenAndServe(":"+port, nil)
	return this.bot.ListenForWebhook("/"), nil
}

func (this *TwatchDog) SendMsg(msg string, chatID int64, buttons Buttons) (int, error) {
	newmsg := tgbotapi.NewMessage(chatID, msg)
	cxt, cancel := context.WithCancel(context.Background())

	fmt.Println("Отправка сообщения", msg, "в чат", chatID)

	buttons.createButtons(&newmsg, this.callback, cancel, 3)
	m, err := this.bot.Send(newmsg)

	timerExist := false
	for _, b := range buttons {
		if timerExist = b.timer > 0; timerExist {
			break
		}
	}

	if timerExist {
		go this.setTimer(m, buttons, cxt, cancel) // таймер кнопки
	}

	return m.MessageID, err
}

//func (this *TwatchDog) AppendChatID(ChatID int64) {
//	if _, ok := this.chatIDs[ChatID]; !ok {
//		this.chatIDs[ChatID] = true
//	}
//}

func (this *TwatchDog) configExist(chatID int64) *Conf {
	strChatID := strconv.FormatInt(chatID, 10)
	if conf, err := this.r.Get(strChatID); err == nil && conf != "" {
		result, _ := this.checkConfig(conf)
		return result
	} else {
		return nil
	}
}

func (this *TwatchDog) checkConfig(xmltxt string) (*Conf, error) {
	value := new(Conf)
	if err := xml.Unmarshal([]byte(xmltxt), &value); err != nil {
		return nil, err
	}

	return value, nil
}

func (this *TwatchDog) setTimer(msg tgbotapi.Message, buttons Buttons, cxt context.Context, cancel context.CancelFunc) {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()

B:
	for {
		select {
		case <-tick.C:
			var button *Button
			for i := 0; i < len(buttons); i++ {
				if buttons[i].timer > 0 {
					buttons[i].timer--

					if buttons[i].timer == 0 {
						button = buttons[i]
					}
				}
			}

			if msg.Chat != nil {
				editmsg := tgbotapi.NewEditMessageText(msg.Chat.ID, msg.MessageID, msg.Text)
				buttons.createButtons(&editmsg, this.callback, cancel, 3)
				this.bot.Send(editmsg)
			}

			// таймер вышел
			if button != nil {
				if h, ok := this.callback[button.ID]; ok {
					h()
					delete(this.callback, button.ID)
				}
			}

		case <-cxt.Done():
			break B
		}
	}
}

func (this *TwatchDog) ReadFile(message *tgbotapi.Message) (data string, err error) {
	//message.Chat.ID
	downloadFilebyID := func(FileID string) {
		var file tgbotapi.File
		if file, err = this.bot.GetFile(tgbotapi.FileConfig{FileID}); err == nil {
			_, fileName := path.Split(file.FilePath)
			filePath := path.Join(os.TempDir(), fileName)
			defer os.Remove(filePath)

			err = this.downloadFile(filePath, file.Link(BotToken))
			if err == nil {
				if dataByte, err := ioutil.ReadFile(filePath); err == nil {
					data = string(dataByte)
				}
			}
		}
	}

	if message.Document != nil {
		downloadFilebyID(message.Document.FileID)
	} else {
		return "", fmt.Errorf("Не поддерживаемый тип данных")
	}

	return data, err
}

func (this *TwatchDog) CallbackQuery(update tgbotapi.Update) bool {
	if update.CallbackQuery == nil {
		return false
	}
	if call, ok := this.callback[update.CallbackQuery.Data]; ok {
		if call != nil {
			call()
		}
		delete(this.callback, update.CallbackQuery.Data)
	}

	return true
}

func (this *TwatchDog) downloadFile(filepath, url string) error {
	resp, err := new(http.Client).Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func (this *TwatchDog) Start(chatID int64, conf *Conf) bool {
	if !atomic.CompareAndSwapInt32(&this.running, 0, 1) {
		return false
	}

	mx := new(sync.Mutex)
	f := func() {
		mx.Lock()

		help := func() {}
		delete := func() {}

		messageID, _ := this.SendMsg("Все ОК?", chatID, Buttons{
			{
				caption: "Ок",
				handler: &delete,
			},
			{
				caption: "Нет",
				handler: &help,
				timer:   conf.Timer,
			},
		})

		delete = func() {
			this.bot.DeleteMessage(tgbotapi.DeleteMessageConfig{
				ChatID:    chatID,
				MessageID: messageID})
			mx.Unlock()
		}
		help = func() {
			this.bot.DeleteMessage(tgbotapi.DeleteMessageConfig{
				ChatID:    chatID,
				MessageID: messageID})

			delete2 := func() {}
			send := func() {}

			// это на случай если "нет" нажали случайно
			messageID2, _ := this.SendMsg("Отправка сообщений скоро начнется", chatID, Buttons{
				{
					caption: "Отмена",
					handler: &delete2,
				},
				{
					caption: "Отправить",
					handler: &send,
					timer:   conf.Timer,
				},
			})

			delete2 = func() {
				this.bot.DeleteMessage(tgbotapi.DeleteMessageConfig{
					ChatID:    chatID,
					MessageID: messageID2})
				mx.Unlock()
			}
			send = func() {
				defer func() {
					mx.Unlock()
				}()

				this.bot.DeleteMessage(tgbotapi.DeleteMessageConfig{
					ChatID:    chatID,
					MessageID: messageID2})

				n := &Notify{
					conf: conf,
				}
				go n.NotifyTelegram()
				go n.NotifyEmail()

				this.SendMsg("Сообщения отправлены", chatID, Buttons{})
			}

		}
	}

	// нам нужно сохранить факт того, что задание выполняется, в случае если бот будет перезапущен
	// что б при старте он начал сразу работать. При Stop файл просто удалим
	this.writeConfToRedis(chatID, conf)

	this.handlers[chatID] = new(scheduler).New(conf, f)
	go this.handlers[chatID].Invoke()

	return true
}

func (this *TwatchDog) writeConfToRedis(chatID int64, conf *Conf) {
	this.Lock()
	defer this.Unlock()

	// на файловую систему я не могу сохранять, особенность хероку
	// https://devcenter.heroku.com/articles/active-storage-on-heroku
	schatID := strconv.FormatInt(chatID, 10)

	// сохраняем
	if conf == nil {
		this.r.Delete(schatID)
	} else {
		if confbyte, err := xml.Marshal(conf); err != nil {
			return
		} else {
			this.r.Set(schatID, string(confbyte), time.Microsecond) // если ttl меньше секунды он проигнорируется
		}
	}
}

func (this *TwatchDog) readAllConfFromRedis() map[string]string {
	result := map[string]string{}
	for _, key := range this.r.Keys() {
		result[key], _ = this.r.Get(key)
	}

	return result
}

// Возобновление работы бота (если приложение завершилось без Stop)
func (this *TwatchDog) Resume() {
	conf := this.readAllConfFromRedis()
	for key, conftxt := range conf {
		if chatID, err := strconv.ParseInt(key, 10, 64); err == nil {
			if conf := this.configExist(chatID); conf != nil {
				this.Start(chatID, conf)
			} else if conftxt != "" {
				ioutil.WriteFile(getConfPath(strconv.FormatInt(chatID, 10)), []byte(conftxt), os.ModePerm)
				if conf := this.configExist(chatID); conf != nil {
					this.Start(chatID, conf)
				}
			}
		}
	}
}

func (this *TwatchDog) Stop(chatID int64) {
	defer atomic.CompareAndSwapInt32(&this.running, 1, 0)

	if sc, ok := this.handlers[chatID]; ok {
		sc.Cancel()
		delete(this.handlers, chatID)
		this.writeConfToRedis(chatID, nil)
	}
}

func (this *TwatchDog) ReStart(chatID int64, conf *Conf) {
	this.Stop(chatID)
	this.Start(chatID, conf)
}

func (this Buttons) breakButtonsByColum(Buttons []tgbotapi.InlineKeyboardButton, countColum int) [][]tgbotapi.InlineKeyboardButton {
	end := 0
	result := [][]tgbotapi.InlineKeyboardButton{}

	for i := 1; i <= int(float64(len(Buttons)/countColum)); i++ {
		end = i * countColum
		start := (i - 1) * countColum
		if end > len(Buttons) {
			end = len(Buttons)
		}

		row := tgbotapi.NewInlineKeyboardRow(Buttons[start:end]...)
		result = append(result, row)
	}
	if len(Buttons)%countColum > 0 {
		row := tgbotapi.NewInlineKeyboardRow(Buttons[end:len(Buttons)]...)
		result = append(result, row)
	}

	return result
}

func (this Buttons) createButtons(msg tgbotapi.Chattable, callback map[string]func(), cancel context.CancelFunc, countColum int) {
	keyboard := tgbotapi.InlineKeyboardMarkup{}
	var Buttons = []tgbotapi.InlineKeyboardButton{}

	switch msg.(type) {
	case *tgbotapi.EditMessageTextConfig:
		msg.(*tgbotapi.EditMessageTextConfig).ReplyMarkup = &keyboard
	case *tgbotapi.MessageConfig:
		msg.(*tgbotapi.MessageConfig).ReplyMarkup = &keyboard
	}

	for _, item := range this {
		handler := item.handler
		if item.ID == "" {
			UUID, _ := uuid.NewV4()
			item.ID = UUID.String()
		}

		callback[item.ID] = func() {
			cancel()
			if handler != nil {
				(*handler)()
			}
		}

		caption := item.caption
		if item.timer > 0 {
			caption = fmt.Sprintf("%s (%02d:%02d:%02d)", item.caption, (item.timer / 3600), (item.timer%3600)/60, (item.timer % 60))
		}

		btn := tgbotapi.NewInlineKeyboardButtonData(caption, item.ID)
		Buttons = append(Buttons, btn)
	}

	keyboard.InlineKeyboard = this.breakButtonsByColum(Buttons, countColum)
}

func getngrokWebhookURL() string {
	// файл Ngrok должен лежать рядом с основным файлом бота
	currentDir, _ := os.Getwd()
	ngrokpath := filepath.Join(currentDir, "ngrok.exe")
	if _, err := os.Stat(ngrokpath); os.IsNotExist(err) {
		return ""
	}

	err := make(chan error, 0)
	result := make(chan string, 0)

	// горутина для запуска ngrok
	go func(chanErr chan<- error) {
		cmd := exec.Command(ngrokpath, "http", "8080")
		err := cmd.Run()
		if err != nil {
			errText := fmt.Sprintf("Произошла ошибка запуска:\n err:%v \n", err.Error())

			if cmd.Stderr != nil {
				if stderr := cmd.Stderr.(*bytes.Buffer).String(); stderr != "" {
					errText += fmt.Sprintf("StdErr:%v", stderr)
				}
			}
			chanErr <- fmt.Errorf(errText)
			close(chanErr)
		}
	}(err)

	type ngrokAPI struct {
		Tunnels []*struct {
			PublicUrl string `json:"public_url"`
		} `json:"tunnels"`
	}

	// горутина для получения адреса
	go func(result chan<- string, chanErr chan<- error) {
		// задумка такая, в горутине выше стартует Ngrok, после запуска поднимается вебсервер на порту 4040
		// и я могу получать url через api. Однако, в текущей горутине я не знаю стартанут там Ngrok или нет, по этому таймер
		// продуем подключиться 5 раз (5 сек) если не получилось, ошибка.
		tryCount := 5
		timer := time.NewTicker(time.Second * 1)
		for range timer.C {
			resp, err := http.Get("http://localhost:4040/api/tunnels")
			if (err == nil && !(resp.StatusCode >= http.StatusOK && resp.StatusCode <= http.StatusIMUsed)) || err != nil {
				if tryCount--; tryCount <= 0 {
					chanErr <- fmt.Errorf("Не удалось получить данные ngrok")
					close(chanErr)
					timer.Stop()
					return
				}
				continue
			}
			body, _ := ioutil.ReadAll(resp.Body)
			resp.Body.Close()

			var ngrok = new(ngrokAPI)
			err = json.Unmarshal(body, &ngrok)
			if err != nil {
				chanErr <- err
				close(chanErr)
				timer.Stop()
				return
			}
			if len(ngrok.Tunnels) == 0 {
				chanErr <- fmt.Errorf("Не удалось получить тунели ngrok")
				close(chanErr)
				timer.Stop()
				return
			}
			for _, url := range ngrok.Tunnels {
				if strings.Index(strings.ToLower(url.PublicUrl), "https") >= 0 {
					result <- url.PublicUrl
					close(result)
					timer.Stop()
					return
				}

			}
			chanErr <- fmt.Errorf("Не нашли https тунель ngrok")
			close(chanErr)
		}
	}(result, err)

	select {
	case <-err:
		return ""
	case r := <-result:
		return r
	}
}
