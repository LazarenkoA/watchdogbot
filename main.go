package main

import (
	"fmt"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api"
	"github.com/ungerik/go-dry"
	"os"
	"path/filepath"
	"strconv"
)

var (
	BotToken = os.Getenv("BotToken")
	WebhookURL = os.Getenv("WebhookURL")
	port = os.Getenv("PORT")
)


func main() {
	// ****************** нужно для хероку ******************
	//port := os.Getenv("PORT")
	//if port == "" {
	//	port = "80"
	//}
	////go http.ListenAndServeTLS(":"+port, "server.crt", "server.key", nil)
	//go http.ListenAndServe(":"+port, nil)
	//http.HandleFunc("/вуи", func(w http.ResponseWriter, r *http.Request) {
	//	fmt.Fprint(w, "working")
	//})
	// ******************


	wd := new(TwatchDog)
	wdUpdate, err := wd.New()
	if err != nil {
		fmt.Println("не удалось подключить бота, ошибка:\n"+ err.Error())
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
				txt := fmt.Sprintf("Привет %v %v!\n" +
					"Для начала работы отправьте мне конфигурационный файл. " +
					"Пример конфига https://github.com/LazarenkoA/watchdogbot/blob/main/exampl_conf.xml", update.Message.From.FirstName, update.Message.From.LastName)
				wd.SendMsg(txt, chatID, Buttons {})
			}
		case "cancel":
			wd.Stop(chatID)
			wd.bot.Send(tgbotapi.NewMessage(chatID, "watchdog остановлен"))
		default:
			if command != "" {
				wd.SendMsg("Команда " + command + " не поддерживается", chatID, Buttons{})
				continue
			}
			var conf *Conf
			if filePath, err := wd.SaveFile(update.Message); err != nil {
				wd.SendMsg("Ошибка сохранения файла:\n" + err.Error(), chatID, Buttons{})
			} else if conf, err = wd.checkConfig(filePath); err != nil {
				wd.SendMsg("Ошибка проверки синтаксиса:\n"+err.Error(), chatID, Buttons{})
			} else if conf = wd.configExist(chatID); conf != nil {
				wd.bot.DeleteMessage(tgbotapi.DeleteMessageConfig{
					ChatID:    chatID,
					MessageID: update.Message.MessageID})

				handleryes := func() {}
				handlerno := func() {}

				messageID, _ := wd.SendMsg("Конфигурационный файл уже существует, заменить его?", chatID, Buttons {
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
					if confPath, err := saveFile(filePath, strconv.FormatInt(chatID, 10)); err != nil {
						wd.SendMsg("Ошибка опирования файла:\n" + err.Error(), chatID, Buttons{})
					} else {
						conf, _  = wd.checkConfig(confPath)
						editmsg := tgbotapi.NewEditMessageText(chatID, messageID, "Конфиг заменен.\nwatchdog перезапущен")
						wd.bot.Send(editmsg)
						wd.ReStart(chatID, conf)
					}
				}
			} else {
				if confPath, err := saveFile(filePath, strconv.FormatInt(chatID, 10)); err != nil {
					wd.SendMsg("Ошибка копирования файла:\n" + err.Error(), chatID, Buttons{})
				} else {
					conf, _ = wd.checkConfig(confPath)
				}
				wd.Start(chatID, conf)
				wd.bot.Send(tgbotapi.NewMessage(chatID, "watchdog запущен"))
			}
		}
	}
}

func saveFile(filePath, chatID string) (string, error) {
	currentDir, _ := os.Getwd()
	dir := filepath.Join(currentDir, chatID)

	os.MkdirAll(dir, os.ModePerm)
	conf := filepath.Join(dir, "conf.xml")

	if err := dry.FileCopy(filePath, conf); err != nil {
		return "", err
	} else {
		return conf, nil
	}
}
