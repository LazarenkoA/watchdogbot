package main

import (
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

var (
	BotToken   = os.Getenv("BotToken")
	WebhookURL = os.Getenv("WebhookURL")
	port       = os.Getenv("PORT")
	redisaddr  = os.Getenv("REDIS")
)

func main() {
	wd := new(TwatchDog)
	wdUpdate, err := wd.New()
	if err != nil {
		fmt.Println("не удалось подключить бота, ошибка:\n" + err.Error())
		os.Exit(1)
	}
	if BotToken == "" {
		fmt.Println("в переменных окружения не задан BotToken")
		os.Exit(1)
	}
	if WebhookURL == "" {
		fmt.Println("в переменных окружения не задан WebhookURL")
		os.Exit(1)
	}
	if redisaddr == "" {
		fmt.Println("в переменных окружения не задан адрес redis")
		os.Exit(1)
	}

	http.HandleFunc("/resetcache", func(rw http.ResponseWriter, r *http.Request) {
		conf := wd.readAllConfFromRedis()
		for key, _ := range conf {
			wd.r.Delete(key)
		}
	})
	http.HandleFunc("/cache/", func(rw http.ResponseWriter, r *http.Request) {
		parts := strings.Split(r.URL.Path, "/")
		key := parts[len(parts)-1]

		if conf, err := wd.r.Get(key); err == nil {
			fmt.Fprint(rw, conf)
		} else {
			fmt.Fprint(rw, "Ошибка: ", err)
		}
	})

	wd.Resume()
	for update := range wdUpdate {
		// обработка команд кнопок
		if wd.CallbackQuery(update) {
			continue
		}
		if update.Message == nil {
			continue
		}

		command := update.Message.Command()
		chatID := update.Message.Chat.ID

		//wd.AppendChatID(chatID)

		switch command {
		case "start":
			if conf := wd.configExist(chatID); conf != nil {
				if wd.Start(chatID, conf) {
					wd.SendMsg("watchdog запущен", chatID, Buttons{})
				} else {
					wd.SendMsg("watchdog уже запущен", chatID, Buttons{})
				}
			} else {
				txt := fmt.Sprintf("Привет %v %v!\n"+
					"Для начала работы отправьте мне конфигурационный файл. "+
					"Пример конфига https://github.com/LazarenkoA/watchdogbot/blob/main/exampl_conf.xml", update.Message.From.FirstName, update.Message.From.LastName)
				wd.SendMsg(txt, chatID, Buttons{})
			}
		case "cancel":
			wd.Stop(chatID)
			wd.bot.Send(tgbotapi.NewMessage(chatID, "watchdog остановлен"))
		default:
			if command != "" {
				wd.SendMsg("Команда "+command+" не поддерживается", chatID, Buttons{})
				continue
			}

			var conf *Conf
			if confdata, err := wd.ReadFile(update.Message); err != nil {
				wd.SendMsg("Ошибка сохранения файла:\n"+err.Error(), chatID, Buttons{})
			} else if conf, err = wd.checkConfig(confdata); err != nil {
				wd.SendMsg("Ошибка проверки синтаксиса:\n"+err.Error(), chatID, Buttons{})
			} else if conf_ := wd.configExist(chatID); conf_ != nil {
				wd.bot.DeleteMessage(tgbotapi.DeleteMessageConfig{
					ChatID:    chatID,
					MessageID: update.Message.MessageID})

				handleryes := func() {}
				handlerno := func() {}

				messageID, _ := wd.SendMsg("Заменить существующую конфигурацию?", chatID, Buttons{
					{
						caption: "Да",
						handler: &handleryes,
					},
					{
						caption: "Нет",
						handler: &handlerno,
						timer:   10,
					},
				})

				handlerno = func() {
					wd.bot.DeleteMessage(tgbotapi.DeleteMessageConfig{
						ChatID:    chatID,
						MessageID: messageID})
				}
				handleryes = func() {
					editmsg := tgbotapi.NewEditMessageText(chatID, messageID, "Конфиг заменен.\nwatchdog перезапущен") // конфиг заменяется при старте
					wd.bot.Send(editmsg)
					wd.ReStart(chatID, conf)
				}
			} else {
				wd.Start(chatID, conf)
				wd.bot.Send(tgbotapi.NewMessage(chatID, "watchdog запущен"))
			}
		}
	}
}

func getConfPath(chatID string) string {
	currentDir, _ := os.Getwd()
	dir := filepath.Join(currentDir, chatID)

	os.MkdirAll(dir, os.ModePerm)
	return filepath.Join(dir, "conf.xml")
}

// heroku logs -n 150 -a botwatchdog | grep "lock" -i --color
// heroku stop dyno -a botwatchdog
